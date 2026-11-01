<script>
	import { onMount } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api.js';
	import { ago, size, speed, timeLeft, when } from '$lib/format.js';
	import { fileState, states, tally, filesUnder } from '$lib/files.js';
	import Chips from '$lib/Chips.svelte';
	import Sheet from '$lib/Sheet.svelte';
	import TreeRows from '$lib/TreeRows.svelte';
	import Bar from '$lib/Bar.svelte';

	let roots = $state([]);
	let scannedAt = $state(null);
	let loadError = $state('');
	let loaded = $state(false);
	let query = $state('');
	let filter = $state('all');
	let selected = $state(null); // file shown in the sheet
	let busy = $state(false);
	let message = $state('');
	const expanded = new SvelteSet();

	// The source is in the URL (?source=books) so a refresh or link keeps it.
	const slug = (r) => r.name.toLowerCase().replace(/[^a-z0-9]+/g, '-');
	const root = $derived(roots.find((r) => slug(r) === page.url.searchParams.get('source')) ?? roots[0]);
	function pick(r) {
		goto(`?source=${slug(r)}`, { replaceState: true, keepFocus: true, noScroll: true });
		selected = null;
	}

	const counts = $derived(root ? tally(root) : null);
	const chipOptions = $derived(
		counts
			? [{ id: 'all', label: 'All', count: counts.files }, ...states.filter((s) => counts[s.id]).map((s) => ({ ...s, count: counts[s.id] }))]
			: []
	);
	const needle = $derived(query.trim().toLowerCase());
	const keep = $derived((f) => (filter === 'all' || fileState(f) === filter) && (!needle || f.name.toLowerCase().includes(needle)));
	const allOpen = $derived(filter !== 'all' || needle !== '');
	const nothing = $derived(root && !filesUnder(root).some(keep));

	async function load() {
		try {
			const data = await api('/api/v1/remote-files/tree');
			roots = data.roots ?? [];
			scannedAt = data.scanned_at;
			loadError = '';
			// Keep the open sheet in step with fresh data.
			if (selected) selected = roots.flatMap((r) => filesUnder(r)).find((f) => f.id === selected.id) ?? null;
		} catch (e) {
			loadError = e.message;
		} finally {
			loaded = true;
		}
	}

	onMount(() => {
		load();
		const t = setInterval(() => {
			if (!document.hidden) load();
		}, 5000);
		return () => clearInterval(t);
	});

	function flash(text) {
		message = text;
		setTimeout(() => (message === text ? (message = '') : null), 4000);
	}

	async function run(fn, done) {
		busy = true;
		try {
			await fn();
			if (done) flash(done);
			await load();
		} catch (e) {
			flash(e.message);
		} finally {
			busy = false;
		}
	}

	async function scan() {
		await run(() => api('/api/v1/sync/scan', 'POST'), 'Scan started');
	}
	const queueFolder = (folder) =>
		run(async () => {
			const r = await api('/api/v1/remote-files/queue-folder', 'POST', { watched_path: root.watched_path, folder_path: folder.path });
			flash(`Queued ${r?.queued ?? 0} files`);
		});
	const retryFolder = (folder) =>
		run(async () => {
			const failed = filesUnder(folder).filter((f) => fileState(f) === 'failed' && f.progress?.job_id);
			for (const f of failed) await api(`/api/v1/jobs/${f.progress.job_id}/retry`, 'POST');
			flash(`Retrying ${failed.length} ${failed.length === 1 ? 'file' : 'files'}`);
		});
	const fileAction = (path, done) => run(() => api(path, 'POST'), done);
	const remotePath = (f) => root.watched_path + f.path.replace(/^\//, '');
</script>

<svelte:head><title>Files · Grabarr</title></svelte:head>

<div class="split">
	<aside>
		<div class="scan muted">
			{scannedAt ? `Scanned ${ago(scannedAt)}` : 'Not scanned yet'} ·
			<button class="link" onclick={scan} disabled={busy}>Scan now</button>
		</div>
		<ul class="sources" aria-label="Sources">
			{#each roots as r (r.watched_path)}
				{@const t = tally(r)}
				<li>
					<button class="source" class:on={r === root} onclick={() => pick(r)} aria-current={r === root ? 'true' : undefined}>
						<b>{r.name}</b>
						<span class="muted small">{r.remote} · {r.watched_path.replace(/\/$/, '').split('/').slice(-2).join('/')}</span>
						<span class="muted small">{t.files} files · {t.copied} copied</span>
					</button>
				</li>
			{/each}
		</ul>
	</aside>

	<main>
		{#if roots.length > 1}
			<div class="mobile-sources">
				<Chips options={roots.map((r) => ({ id: r.watched_path, label: r.name }))} value={root?.watched_path} onchange={(id) => pick(roots.find((r) => r.watched_path === id))} />
			</div>
		{/if}
		<div class="tools">
			<input type="search" placeholder="Search files" bind:value={query} aria-label="Search files" />
			<Chips options={chipOptions} value={filter} onchange={(id) => (filter = id)} />
		</div>
		{#if message}<p class="message" role="status">{message}</p>{/if}

		{#if !loaded}
			<p class="muted pad">Loading…</p>
		{:else if loadError}
			<p class="error pad">Couldn't load files: {loadError}</p>
		{:else if !root}
			<p class="muted pad">Nothing on the seedbox yet. Files show up here after the next scan.</p>
		{:else if nothing}
			<p class="muted pad">No files match.</p>
		{:else}
			<div class="head" aria-hidden="true"><span>Name</span><span>Size</span><span>Progress</span><span></span></div>
			<div class="tree">
				<TreeRows node={root} {keep} {allOpen} {expanded} {busy} onfile={(f) => (selected = f)} onqueue={queueFolder} onretry={retryFolder} />
			</div>
		{/if}
	</main>
</div>

{#if selected}
	{@const f = selected}
	{@const st = fileState(f)}
	{@const p = f.progress}
	<Sheet label={f.name} onclose={() => (selected = null)}>
		<h2>{f.name}</h2>
		<p class="muted small">{size(f.size_bytes)}{p?.job_id ? ` · job #${p.job_id}` : ''}{p?.created_at ? ` · queued ${ago(p.created_at)}` : ''}</p>
		{#if st === 'copying' && p}
			<Bar value={p.percentage} />
			<p class="muted small">{[`${Math.round(p.percentage)}%`, speed(p.transfer_speed), timeLeft(p.total_bytes, p.transferred_bytes, p.transfer_speed)].filter(Boolean).join(' · ')}</p>
		{/if}
		{#if p?.error_message}<p class="error">{p.error_message}</p>{/if}
		<dl>
			<dt>On the seedbox</dt>
			<dd>{remotePath(f)}</dd>
			{#if p?.completed_at}<dt>Copied</dt><dd>{when(p.completed_at)}</dd>{/if}
		</dl>
		<div class="actions">
			{#if st === 'seedbox'}
				<button disabled={busy} onclick={() => fileAction(`/api/v1/remote-files/${f.id}/queue`, 'Queued')}>Queue</button>
				<button class="secondary" disabled={busy} onclick={() => fileAction(`/api/v1/remote-files/${f.id}/ignore`, 'Ignored')}>Ignore</button>
			{:else if st === 'ignored'}
				<button class="secondary" disabled={busy} onclick={() => fileAction(`/api/v1/remote-files/${f.id}/restore`, 'Restored')}>Restore</button>
			{:else if st === 'failed'}
				<button disabled={busy} onclick={() => fileAction(`/api/v1/jobs/${p.job_id}/retry`, 'Retrying')}>Retry</button>
			{:else if (st === 'queued' || st === 'copying') && p?.job_id}
				<button class="danger" disabled={busy} onclick={() => fileAction(`/api/v1/jobs/${p.job_id}/cancel`, 'Cancelled')}>Cancel copy</button>
			{/if}
		</div>
	</Sheet>
{/if}

<style>
	.split {
		max-width: var(--measure);
		margin: 0 auto;
	}
	aside {
		padding: 0 var(--gutter);
	}
	.scan {
		font-size: 0.9rem;
		margin-bottom: 0.6rem;
	}
	.sources {
		display: none;
	}
	.mobile-sources {
		padding: 0 var(--gutter) 0.6rem;
	}
	.tools {
		display: flex;
		flex-direction: column;
		gap: 0.6rem;
		padding: 0 var(--gutter) 0.7rem;
	}
	input[type='search'] {
		font-size: 16px;
		width: 100%;
	}
	.message {
		margin: 0 var(--gutter) 0.6rem;
		font-size: 0.9rem;
	}
	.pad {
		padding: 1rem var(--gutter);
	}
	.head {
		display: none;
	}
	.small {
		font-size: 0.85rem;
	}
	.link {
		font-size: inherit;
		color: var(--text);
		text-decoration: underline;
	}
	h2 {
		font-size: 1.15rem;
		letter-spacing: -0.02em;
		margin: 0 0 0.2rem;
		overflow-wrap: anywhere;
	}
	dl {
		margin: 0.8rem 0;
		font-size: 0.9rem;
	}
	dt {
		color: var(--muted);
		font-size: 0.85rem;
	}
	dd {
		margin: 0 0 0.5rem;
		overflow-wrap: anywhere;
	}
	.actions {
		display: flex;
		gap: 0.6rem;
		flex-wrap: wrap;
	}

	@media (min-width: 900px) {
		.split {
			max-width: var(--measure-wide);
			display: grid;
			grid-template-columns: var(--sidebar) minmax(0, 1fr);
			min-height: calc(100vh - 60px);
		}
		aside {
			border-right: 1px solid var(--line);
			padding: 1rem var(--gutter);
		}
		.sources {
			display: block;
			list-style: none;
			margin: 0;
			padding: 0;
		}
		.source {
			display: flex;
			flex-direction: column;
			align-items: flex-start;
			width: 100%;
			text-align: left;
			background: none;
			color: var(--text);
			border-radius: var(--radius-sm);
			padding: 0.6rem 0.75rem;
			margin-bottom: 4px;
			font-weight: 400;
		}
		.source b {
			font-weight: 600;
		}
		.source.on {
			background: var(--surface);
		}
		.mobile-sources {
			display: none;
		}
		main {
			padding: 1rem 24px;
			min-width: 0;
		}
		.tools {
			flex-direction: row;
			align-items: center;
			padding: 0 0 0.8rem;
		}
		input[type='search'] {
			width: 280px;
			flex: none;
		}
		.message,
		.pad {
			margin-left: 0;
			padding-left: 0;
		}
		.head {
			display: grid;
			grid-template-columns: minmax(0, 1fr) 90px 300px 110px;
			column-gap: 12px;
			color: var(--muted);
			font-size: 0.85rem;
			padding-bottom: 0.4rem;
		}
		.head span:nth-child(2) {
			text-align: right;
		}
	}
</style>
