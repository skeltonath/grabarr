<script>
	/**
	 * A horizontally scrolling row of filter chips; one is selected.
	 * @type {{ options: {id: string, label: string, count?: number}[], value: string, onchange: (id: string) => void }}
	 */
	let { options, value, onchange } = $props();
</script>

<div class="chips" role="radiogroup">
	{#each options as o (o.id)}
		<button
			class="chip"
			class:on={o.id === value}
			role="radio"
			aria-checked={o.id === value}
			onclick={() => onchange(o.id)}
		>
			{o.label}{#if o.count !== undefined}<span class="n">{o.count}</span>{/if}
		</button>
	{/each}
</div>

<style>
	.chips {
		display: flex;
		gap: 8px;
		overflow-x: auto;
		scrollbar-width: none;
		/* A sideways-scrolling row clips vertically too; the padding keeps
		   the chips' borders inside it. */
		padding: 2px 0;
	}
	/* Phones scroll the row sideways; desktop has room to wrap. */
	@media (min-width: 900px) {
		.chips {
			flex-wrap: wrap;
		}
	}
	.chip {
		flex: none;
		background: var(--surface);
		color: var(--muted);
		border: 1px solid var(--line);
		padding: 0.35rem 0.85rem;
		font-size: 0.9rem;
		font-weight: 600;
		min-height: 36px;
	}
	.chip.on {
		color: var(--text);
		border-color: var(--text);
	}
	.n {
		margin-left: 0.4em;
		font-variant-numeric: tabular-nums;
		font-weight: 400;
	}
</style>
