<script>
	/** Progress bar: a --line track with an --accent fill. */
	let { value = 0, thin = false } = $props();
</script>

<div class="track" class:thin role="progressbar" aria-valuenow={Math.round(value)} aria-valuemin="0" aria-valuemax="100">
	<div class="fill" style:width="{Math.min(100, Math.max(0, value))}%"></div>
</div>

<style>
	.track {
		height: 10px;
		border-radius: 999px;
		background: var(--line);
		overflow: hidden;
	}
	.track.thin {
		height: 6px;
	}
	.fill {
		height: 100%;
		background: var(--accent);
		border-radius: 999px;
		transition: width 0.4s ease;
	}
	@media (prefers-reduced-motion: reduce) {
		.fill {
			transition: none;
		}
	}
</style>
