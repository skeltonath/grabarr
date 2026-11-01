<script>
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, apiPage } from '$lib/api.js';
	import { ago, category, hostPath, size, speed, timeLeft, when } from '$lib/format.js';
	import { status } from '$lib/status.svelte.js';
	import Chips from '$lib/Chips.svelte';
	import Sheet from '$lib/Sheet.svelte';
	import Bar from '$lib/Bar.svelte';

	const PAGE = 50;
	let jobs = $state([]);
	let total = $state(0);
	let shown = $state(PAGE); // how many rows to keep loaded across refreshes
	let loaded = $state(false);
	let loadError = $state('');
	let text = $state('');
	let busy = $state(false);
	let message = $state('');
	let wide = $state(false);

	// Filter and selection live in the URL so refresh and back keep them.
	const filter = $derived(page.url.searchParams.get('status') ?? 'all');
	const selectedId = $derived(Number(page.url.searchParams.get('job')) || null);
	function setParam(key, value) {
		const u = new URL(page.url);
		if (value === null || value === 'all') u.searchParams.delete(key);
		else u.searchParams.set(key, String(value));
		goto(u.pathname + u.search, { replaceState: key === 'job', keepFocus: true, noScroll: true });
	}

	const c = $derived(status.jobs);
	const chipOptions = $derived(
		[
			{ id: 'all', label: 'All', count: c?.total_jobs },
			{ id: 'running', label: 'Running', count: c?.running_jobs },
			{ id: 'queued', label: 'Queued', count: c ? c.queued_jobs + c.pending_jobs : undefined },
			{ id: 'failed', label: 'Failed', count: c?.failed_jobs },
			{ id: 'completed', label: 'Done', count: c?.completed_jobs },
			{ id: 'cancelled', label: 'Cancelled', count: c?.cancelled_jobs }
		].filter((o) => o.id === 'all' || o.id === filter || o.count)
	);

	const needle = $derived(text.trim().toLowerCase());
	const visible = $derived(needle ? jobs.filter((j) => j.name.toLowerCase().includes(needle)) : jobs);
	let fetched = $state(null); // a selected job that isn't in the loaded rows
	const selected = $derived(jobs.find((j) => j.id === selectedId) ?? (fetched?.id === selectedId ? fetched : null));

	async function load() {
		const q = new URLSearchParams({ limit: String(shown), offset: '0', sort_by: 'created_at', sort_order: 'desc' });
		if (filter !== 'all') q.set('status', filter);
		try {
			const { data, pagination } = await apiPage(`/api/v1/jobs?${q}`);
			jobs = data;
			total = pagination?.total ?? data.length;
			loadError = '';
			if (selectedId && !data.some((j) => j.id === selectedId)) {
				fetched = await api(`/api/v1/jobs/${selectedId}`).catch(() => null);
			}
		} catch (e) {
			loadError = e.message;
		} finally {
			loaded = true;
		}
	}

	// Reload from the top when the filter changes.
	$effect(() => {
		filter;
		shown = PAGE;
		load();
	});

	onMount(() => {
		const mq = matchMedia('(min-width: 900px)');
		wide = mq.matches;
		const onmq = () => (wide = mq.matches);
		mq.addEventListener('change', onmq);
		const t = setInterval(() => {
			if (!document.hidden) load();
		}, 5000);
		return () => {
			clearInterval(t);
			mq.removeEventListener('change', onmq);
		};
	});

	function more() {
		shown += PAGE;
		load();
	}

	function flash(m) {
		message = m;
		setTimeout(() => (message === m ? (message = '') : null), 4000);
	}

	async function act(job, action) {
		busy = true;
		try {
			if (action === 'delete') {
				await api(`/api/v1/jobs/${job.id}`, 'DELETE');
				setParam('job', null);
				flash('Deleted');
			} else {
				await api(`/api/v1/jobs/${job.id}/${action}`, 'POST');
				flash(action === 'retry' ? 'Retrying' : 'Cancelled');
			}
			await load();
		} catch (e) {
			flash(e.message);
		} finally {
			busy = false;
		}
	}

	const live = (j) => j.status === 'running';
	const progressText = (j) =>
		[`${Math.round(j.progress?.percentage ?? 0)}%`, speed(j.transfer_speed), timeLeft(j.file_size, j.transferred_bytes, j.transfer_speed)]
			.filter(Boolean)
			.join(' · ');
	const statusText = (j) =>
		({
			queued: 'queued',
			pending: 'starting',
			failed: `failed · ${j.retries} of ${j.max_retries} tries`,
			cancelled: 'cancelled',
			completed: j.started_at && j.completed_at ? `done in ${Math.max(1, Math.round((new Date(j.completed_at) - new Date(j.started_at)) / 60000))} min` : 'done'
		})[j.status] ?? j.status;
</script>

<svelte:head><title>Jobs · Grabarr</title></svelte:head>

{#snippet detail(j)}
	<h2>{j.name}</h2>
	<p class="muted small">Job #{j.id}{j.metadata?.category ? ` · ${category(j.metadata.category)}` : ''} · {size(j.file_size)} · added {ago(j.created_at)}</p>
	{#if live(j)}
		<Bar value={j.progress?.percentage ?? 0} />
		<p class="muted small">{progressText(j)}</p>
	{:else}
		<p class="state {j.status}">{statusText(j)}</p>
	{/if}
	{#if j.error_message}<p class="error">{j.error_message}</p>{/if}
	<dl>
		<dt>From</dt>
		<dd>{j.remote_path}</dd>
		<dt>To</dt>
		<dd>{hostPath(j.local_path)}</dd>
		{#if j.started_at}<dt>Started</dt><dd>{when(j.started_at)}</dd>{/if}
		{#if j.completed_at}<dt>Finished</dt><dd>{when(j.completed_at)}</dd>{/if}
		{#if j.retries}<dt>Tries</dt><dd>{j.retries} of {j.max_retries}</dd>{/if}
	</dl>
	<div class="actions">
		{#if j.status === 'failed' || j.status === 'cancelled'}
			<button disabled={busy} onclick={() => act(j, 'retry')}>Retry</button>
			<button class="danger" disabled={busy} onclick={() => act(j, 'delete')}>Delete</button>
		{:else if j.status === 'completed'}
			<button class="secondary" disabled={busy} onclick={() => act(j, 'delete')}>Remove from list</button>
		{:else}
			<button class="danger" disabled={busy} onclick={() => act(j, 'cancel')}>Cancel copy</button>
		{/if}
	</div>
{/snippet}

<div class="split">
	<main>
		<div class="tools">
			<Chips options={chipOptions} value={filter} onchange={(id) => setParam('status', id)} />
			<input type="search" placeholder="Filter jobs" bind:value={text} aria-label="Filter jobs" />
		</div>
		{#if message}<p class="message" role="status">{message}</p>{/if}

		{#if !loaded}
			<p class="muted pad">Loading…</p>
		{:else if loadError}
			<p class="error pad">Couldn't load jobs: {loadError}</p>
		{:else if visible.length === 0}
			<p class="muted pad">{needle ? 'No jobs match.' : filter === 'all' ? 'No jobs yet. They appear when files are queued.' : 'No jobs here.'}</p>
		{:else}
			<div class="head" aria-hidden="true"><span>Name</span><span>Progress</span><span>Size</span><span>Added</span></div>
			<ul class="list">
				{#each visible as j (j.id)}
					<li>
						<button class="row" class:on={j.id === selectedId} onclick={() => setParam('job', j.id)}>
							<span class="name">{j.name}</span>
							<span class="prog">
								{#if live(j)}
									<Bar thin value={j.progress?.percentage ?? 0} />
									<span class="muted small">{progressText(j)}</span>
								{:else}
									<span class="state {j.status}">{statusText(j)}</span>
								{/if}
							</span>
							<span class="meta muted small">{size(j.file_size)}{j.metadata?.category ? ` · ${category(j.metadata.category)}` : ''}</span>
							<span class="added muted small" title={when(j.created_at)}>{ago(j.created_at)}</span>
						</button>
					</li>
				{/each}
			</ul>
			{#if jobs.length < total && !needle}
				<div class="more"><button class="secondary" onclick={more}>Load more</button></div>
			{/if}
		{/if}
	</main>

	{#if wide}
		<section class="pane" aria-label="Job details">
			{#if selected}
				{@render detail(selected)}
			{:else}
				<p class="muted">Pick a job to see its details.</p>
			{/if}
		</section>
	{/if}
</div>

{#if !wide && selected}
	<Sheet label={selected.name} onclose={() => setParam('job', null)}>
		{@render detail(selected)}
	</Sheet>
{/if}

<style>
	.split {
		max-width: var(--measure);
		margin: 0 auto;
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
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.row {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		grid-template-areas: 'name name' 'prog prog' 'meta added';
		gap: 4px 12px;
		width: 100%;
		text-align: left;
		background: none;
		color: var(--text);
		font-weight: 400;
		border-radius: 0;
		border-top: 1px solid var(--line);
		padding: 0.75rem var(--gutter);
	}
	.name {
		grid-area: name;
		font-weight: 600;
		overflow: hidden;
		display: -webkit-box;
		-webkit-line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow-wrap: anywhere;
	}
	.prog {
		grid-area: prog;
		display: flex;
		flex-direction: column;
		gap: 3px;
	}
	.meta {
		grid-area: meta;
	}
	.added {
		grid-area: added;
		text-align: right;
		white-space: nowrap;
	}
	.small {
		font-size: 0.85rem;
		font-variant-numeric: tabular-nums;
	}
	.state {
		font-size: 0.85rem;
		font-weight: 600;
		color: var(--muted);
		margin: 0;
	}
	.state.failed {
		color: var(--danger);
	}
	.state.completed {
		color: var(--accent);
	}
	.state.pending {
		color: var(--warm);
	}
	.more {
		text-align: center;
		padding: 1rem;
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
	.pane p.state {
		margin: 0.4rem 0;
	}

	@media (min-width: 900px) {
		.split {
			max-width: var(--measure-wide);
			display: grid;
			grid-template-columns: minmax(0, 1.6fr) minmax(320px, 1fr);
			min-height: calc(100vh - 60px);
		}
		main {
			border-right: 1px solid var(--line);
			padding: 1rem 24px;
			min-width: 0;
		}
		.tools {
			flex-direction: row;
			align-items: center;
			padding: 0 0 0.8rem;
		}
		.tools input {
			width: 220px;
			flex: none;
			margin-left: auto;
		}
		.message,
		.pad {
			margin-left: 0;
			padding-left: 0;
		}
		.head,
		.row {
			display: grid;
			grid-template-columns: minmax(0, 1fr) 190px 160px 64px;
			grid-template-areas: 'name prog meta added';
			column-gap: 16px;
			align-items: center;
		}
		.head {
			color: var(--muted);
			font-size: 0.85rem;
			padding: 0 8px 0.4rem;
		}
		.row {
			padding: 0.6rem 8px;
		}
		.row.on {
			background: var(--surface);
			border-radius: var(--radius-sm);
		}
		.name {
			-webkit-line-clamp: 1;
		}
		.meta {
			text-align: right;
			white-space: nowrap;
			overflow: hidden;
			text-overflow: ellipsis;
		}
		.pane {
			padding: 1.2rem 24px;
			min-width: 0;
		}
	}
</style>
