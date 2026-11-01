<script>
	import { onMount } from 'svelte';
	import { api, signOut } from '$lib/api.js';

	const groups = [
		{ title: 'Transfers', keys: ['jobs.max_concurrent', 'gatekeeper.seedbox.bandwidth_limit_mbps', 'gatekeeper.cache_disk.max_usage_percent', 'jobs.max_retries'] },
		{ title: 'Seedbox scanning', keys: ['sync.enabled', 'sync.scan_interval'] },
		{ title: 'Job history', keys: ['jobs.cleanup_completed_after', 'jobs.cleanup_failed_after'] }
	];
	const durationUnits = [
		{ id: 'min', label: 'min', seconds: 60 },
		{ id: 'h', label: 'hours', seconds: 3600 },
		{ id: 'd', label: 'days', seconds: 86400 }
	];

	let settings = $state([]);
	let loadError = $state('');
	let saveError = $state('');
	let saving = $state(false);
	let saved = $state(false);
	let me = $state('');
	// Unsaved edits by key. null means "reset to what the file says".
	let draft = $state({});
	// The unit each duration is edited in, by key.
	let units = $state({});

	const byKey = $derived(Object.fromEntries(settings.map((s) => [s.key, s])));
	const dirty = $derived(Object.keys(draft).length);

	function bestUnit(seconds) {
		if (seconds % 86400 === 0) return 'd';
		if (seconds % 3600 === 0) return 'h';
		return 'min';
	}
	const unitSeconds = (key) => durationUnits.find((u) => u.id === units[key]).seconds;

	async function load() {
		try {
			const data = await api('/api/v1/settings');
			settings = data.settings;
			for (const s of settings) if (s.kind === 'duration' && !units[s.key]) units[s.key] = bestUnit(s.value);
			loadError = '';
		} catch (e) {
			loadError = e.message;
		}
	}
	onMount(async () => {
		load();
		const r = await fetch('/auth/me').then((r) => (r.ok ? r.json() : null)).catch(() => null);
		me = r?.data?.name ?? '';
	});

	const current = (s) => (s.key in draft ? (draft[s.key] ?? s.config_value) : s.value);

	function stage(s, value) {
		saved = false;
		if (value === s.value) delete draft[s.key];
		else draft[s.key] = value;
	}
	function reset(s) {
		saved = false;
		if (s.value === s.config_value) delete draft[s.key];
		else draft[s.key] = null;
	}

	function invalid(s) {
		const v = current(s);
		if (s.kind === 'bool') return '';
		if (!Number.isFinite(v)) return 'Enter a number.';
		if (s.min !== undefined && v < s.min) return `At least ${shown(s, s.min)}.`;
		if (s.max !== undefined && v > s.max) return `At most ${shown(s, s.max)}.`;
		return '';
	}
	const anyInvalid = $derived(settings.some((s) => s.key in draft && invalid(s)));

	/** A value as the person reads it: "3 jobs", "7 days", "on". */
	function shown(s, v) {
		if (s.kind === 'bool') return v ? 'on' : 'off';
		if (s.kind === 'duration') {
			const u = durationUnits.find((u) => u.id === bestUnit(v));
			return `${v / u.seconds} ${u.label}`;
		}
		return `${v}${s.unit ? (s.unit === '%' ? '%' : ` ${s.unit}`) : ''}`;
	}

	async function save() {
		saving = true;
		saveError = '';
		try {
			const data = await api('/api/v1/settings', 'PATCH', draft);
			settings = data.settings;
			draft = {};
			saved = true;
		} catch (e) {
			saveError = e.message;
		} finally {
			saving = false;
		}
	}
</script>

<svelte:head><title>Settings · Grabarr</title></svelte:head>

<main>
	{#if loadError}
		<p class="error">Couldn't load settings: {loadError}</p>
	{:else if settings.length}
		{#each groups as g (g.title)}
			<h2>{g.title}</h2>
			<div class="card group">
				{#each g.keys.filter((k) => byKey[k]) as key (key)}
					{@const s = byKey[key]}
					{@const v = current(s)}
					{@const err = s.key in draft ? invalid(s) : ''}
					<div class="setting">
						<div class="top">
							<label for={s.key}>{s.label}</label>
							{#if s.kind === 'bool'}
								<input id={s.key} type="checkbox" role="switch" class="switch" checked={v} onchange={(e) => stage(s, e.currentTarget.checked)} />
							{:else if s.kind === 'duration'}
								<span class="control">
									<input
										id={s.key}
										type="number"
										inputmode="numeric"
										min="0"
										value={v / unitSeconds(s.key)}
										oninput={(e) => stage(s, Math.round(e.currentTarget.valueAsNumber * unitSeconds(s.key)))}
									/>
									<select bind:value={units[s.key]} aria-label="{s.label} unit">
										{#each durationUnits as u (u.id)}<option value={u.id}>{u.label}</option>{/each}
									</select>
								</span>
							{:else}
								<span class="control">
									<input id={s.key} type="number" inputmode="numeric" min={s.min} max={s.max} value={v} oninput={(e) => stage(s, e.currentTarget.valueAsNumber)} />
									{#if s.unit}<span class="muted unit">{s.unit}</span>{/if}
								</span>
							{/if}
						</div>
						<p class="muted note">{s.description}</p>
						{#if err}
							<p class="error note">{err}</p>
						{:else if s.key in draft && draft[s.key] === null}
							<p class="muted note">Goes back to the file's {shown(s, s.config_value)} when you save.</p>
						{:else if s.overridden}
							<p class="muted note">Changed here · the file says {shown(s, s.config_value)} · <button class="link" onclick={() => reset(s)}>Reset</button></p>
						{/if}
					</div>
				{/each}
			</div>
		{/each}
		<p class="muted who">
			{me ? `Signed in as ${me}` : 'Signed in'} · <button class="link" onclick={signOut}>Sign out</button>
		</p>
	{:else}
		<p class="muted">Loading…</p>
	{/if}
</main>

{#if dirty || saved || saveError}
	<div class="dock">
		<span class="muted status" role="status">
			{#if saveError}<span class="error">{saveError}</span>{:else if dirty}{dirty} unsaved {dirty === 1 ? 'change' : 'changes'}{:else}Saved{/if}
		</span>
		{#if dirty}
			<button class="secondary" disabled={saving} onclick={() => ((draft = {}), (saveError = ''))}>Discard</button>
			<button disabled={saving || anyInvalid} onclick={save}>{saving ? 'Saving…' : 'Save'}</button>
		{/if}
	</div>
{/if}

<style>
	main {
		max-width: var(--measure);
		margin: 0 auto;
		padding: 0.4rem var(--gutter) 7rem;
	}
	h2 {
		font-size: 1.1rem;
		letter-spacing: -0.02em;
		margin: 1rem 0 0.6rem;
	}
	.group {
		padding: 0;
	}
	.setting {
		padding: 0.8rem 1rem;
	}
	.setting + .setting {
		border-top: 1px solid var(--line);
	}
	.top {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	label {
		flex: 1;
		font-weight: 600;
	}
	.control {
		display: flex;
		align-items: center;
		gap: 6px;
	}
	input[type='number'] {
		width: 5.5rem;
		text-align: right;
		font-size: 16px;
		font-variant-numeric: tabular-nums;
	}
	select {
		font-size: 16px;
	}
	/* Fixed width so the inputs line up whatever the unit ("%", "attempts"). */
	.unit,
	select {
		width: 5.6rem;
	}
	.unit {
		font-size: 0.9rem;
	}
	.note {
		font-size: 0.85rem;
		margin: 0.25rem 0 0;
	}
	.link {
		font-size: inherit;
		color: var(--text);
		text-decoration: underline;
	}
	.who {
		font-size: 0.9rem;
		margin-top: 1.4rem;
	}
	.switch {
		appearance: none;
		width: 46px;
		height: 28px;
		border-radius: 999px;
		background: var(--line);
		border: 0;
		padding: 0;
		position: relative;
		cursor: pointer;
		transition: background 0.2s ease;
	}
	.switch::after {
		content: '';
		position: absolute;
		top: 3px;
		left: 3px;
		width: 22px;
		height: 22px;
		border-radius: 50%;
		background: var(--surface);
		transition: transform 0.2s ease;
	}
	.switch:checked {
		background: var(--accent);
	}
	.switch:checked::after {
		transform: translateX(18px);
		background: var(--accent-text);
	}
	.switch:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	@media (prefers-reduced-motion: reduce) {
		.switch,
		.switch::after {
			transition: none;
		}
	}
	.dock {
		position: fixed;
		left: 0;
		right: 0;
		bottom: 0;
		background: var(--bg);
		border-top: 1px solid var(--line);
		padding: 0.75rem var(--gutter) calc(0.75rem + env(safe-area-inset-bottom));
		display: flex;
		align-items: center;
		gap: 0.6rem;
		justify-content: center;
	}
	.status {
		flex: 1;
		max-width: calc(var(--measure) - 220px);
		font-size: 0.9rem;
	}
</style>
