<script>
	import Bar from './Bar.svelte';
	import Self from './TreeRows.svelte';
	import { visibleChildren, tally, summary, fileState } from './files.js';
	import { size, speed, timeLeft, pct } from './format.js';

	/**
	 * One level of the file tree. Folders toggle open (or are all open while
	 * filtering); a file opens its detail sheet.
	 */
	let { node, depth = 0, keep, allOpen, expanded, onfile, ondownload, onretry, busy } = $props();

	const rows = $derived(visibleChildren(node, keep));
	const stateLabel = { seedbox: 'on seedbox', queued: 'queued', copying: 'copying', copied: 'copied', failed: 'failed', ignored: 'ignored' };

	function toggle(path) {
		if (expanded.has(path)) expanded.delete(path);
		else expanded.add(path);
	}
</script>

{#each rows as row (row.key)}
	{#if row.folder}
		{@const t = tally(row.folder)}
		{@const open = allOpen || expanded.has(row.folder.path)}
		{@const active = t.copied + t.copying + t.queued > 0}
		<div class="row folder" style:--depth={depth}>
			<button class="name" onclick={() => toggle(row.folder.path)} aria-expanded={open}>
				<span class="caret" class:open aria-hidden="true">▸</span><span class="clamp">{row.label}</span>
			</button>
			<span class="size">{size(t.bytes)}</span>
			<span class="progress">
				{#if active}<span class="b"><Bar value={pct(t.doneBytes, t.wantedBytes)} /></span>{/if}
				<span class="sum" class:failed={t.failed && !t.seedbox}>{summary(t)}</span>
			</span>
			<span class="act">
				{#if t.seedbox}
					<button class="secondary small" disabled={busy} onclick={() => ondownload(row.folder)}>Download now</button>
				{:else if t.failed}
					<button class="secondary small" disabled={busy} onclick={() => onretry(row.folder)}>Retry {t.failed}</button>
				{/if}
			</span>
		</div>
		{#if open}
			<Self node={row.folder} depth={depth + 1} {keep} {allOpen} {expanded} {onfile} {ondownload} {onretry} {busy} />
		{/if}
	{:else}
		{@const f = row.file}
		{@const st = fileState(f)}
		{@const p = f.progress}
		<button class="row file" style:--depth={depth} onclick={() => onfile(f)}>
			<span class="name"><span class="clamp">{f.name}</span></span>
			<span class="size">{size(f.size_bytes)}</span>
			<span class="progress">
				{#if st === 'copying' && p}
					<span class="b"><Bar thin value={p.percentage} /></span>
					<span class="sum">{[`${Math.round(p.percentage)}%`, speed(p.transfer_speed), timeLeft(p.total_bytes, p.transferred_bytes, p.transfer_speed)].filter(Boolean).join(' · ')}</span>
				{/if}
			</span>
			<span class="act"><span class="state {st}">{stateLabel[st]}</span></span>
		</button>
	{/if}
{/each}

<style>
	.row {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		grid-template-areas: 'name size' 'b b' 'sum act';
		column-gap: 12px;
		row-gap: 4px;
		align-items: center;
		width: 100%;
		padding: 0.7rem var(--gutter) 0.7rem calc(var(--gutter) + var(--depth) * 18px);
		border-top: 1px solid var(--line);
		background: none;
		color: var(--text);
		font: inherit;
		font-weight: 400;
		text-align: left;
		border-radius: 0;
	}
	.file {
		grid-template-areas: 'name act' 'b b' 'sum sum';
		cursor: pointer;
	}
	.name {
		grid-area: name;
		display: flex;
		gap: 0.4rem;
		min-width: 0;
		background: none;
		color: inherit;
		padding: 0;
		font-weight: 400;
		text-align: left;
		border-radius: 0;
	}
	.folder .name {
		font-weight: 600;
		cursor: pointer;
		min-height: 32px;
		align-items: center;
	}
	.clamp {
		overflow: hidden;
		display: -webkit-box;
		-webkit-line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow-wrap: anywhere;
	}
	.caret {
		color: var(--muted);
		transition: transform 0.2s ease;
		flex: none;
	}
	.caret.open {
		transform: rotate(90deg);
	}
	@media (prefers-reduced-motion: reduce) {
		.caret {
			transition: none;
		}
	}
	.size {
		grid-area: size;
		color: var(--muted);
		font-size: 0.85rem;
		font-variant-numeric: tabular-nums;
		text-align: right;
		white-space: nowrap;
	}
	.file .size {
		display: none;
	}
	/* On phones the bar and summary take their own grid rows; on desktop they
	   stack inside one column. */
	.progress {
		display: contents;
	}
	.b {
		grid-area: b;
		margin-top: 4px;
	}
	.sum {
		grid-area: sum;
	}
	.sum {
		color: var(--muted);
		font-size: 0.85rem;
		font-variant-numeric: tabular-nums;
	}
	.sum.failed {
		color: var(--danger);
	}
	.act {
		grid-area: act;
		justify-self: end;
	}
	.act:empty {
		display: none;
	}
	.folder .act {
		align-self: end;
	}
	button.small {
		padding: 0.4rem 0.9rem;
		font-size: 0.9rem;
		min-height: 36px;
		white-space: nowrap;
	}
	.state {
		font-size: 0.85rem;
		font-weight: 600;
		white-space: nowrap;
		color: var(--muted);
	}
	.state.copying {
		color: var(--warm);
	}
	.state.copied {
		color: var(--accent);
	}
	.state.failed {
		color: var(--danger);
	}

	@media (min-width: 900px) {
		.row,
		.file {
			grid-template-columns: minmax(0, 1fr) 90px 300px 140px;
			grid-template-areas: 'name size progress act';
			padding: 0.55rem 0 0.55rem calc(var(--depth) * 20px);
		}
		.file .size {
			display: block;
		}
		.progress {
			grid-area: progress;
			display: flex;
			flex-direction: column;
			gap: 4px;
			min-width: 0;
		}
		.b {
			margin-top: 0;
		}
		.clamp {
			-webkit-line-clamp: 1;
		}
		.folder .act {
			margin-top: 0;
		}
	}
</style>
