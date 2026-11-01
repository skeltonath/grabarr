<script>
	/** A bottom sheet over a scrim, with a handle. Escape or the scrim closes it. */
	let { label, onclose, children } = $props();

	function onkeydown(e) {
		if (e.key === 'Escape') onclose();
	}
</script>

<svelte:window {onkeydown} />

<div class="backdrop" onclick={onclose} role="presentation"></div>
<div class="sheet" role="dialog" aria-modal="true" aria-label={label}>
	<button class="handle" onclick={onclose} aria-label="Close"><span></span></button>
	<div class="scroll">{@render children()}</div>
</div>

<style>
	.backdrop {
		position: fixed;
		inset: 0;
		z-index: 30;
		background: var(--scrim);
		touch-action: none;
	}
	.sheet {
		position: fixed;
		left: 0;
		right: 0;
		bottom: 0;
		z-index: 31;
		max-width: var(--measure);
		max-height: 88vh;
		margin: 0 auto;
		background: var(--bg);
		border: 1px solid var(--line);
		border-bottom: 0;
		border-radius: 18px 18px 0 0;
		display: flex;
		flex-direction: column;
		padding: 0 var(--gutter) calc(1.2rem + env(safe-area-inset-bottom));
		animation: rise 0.25s ease;
	}
	@keyframes rise {
		from {
			transform: translateY(40px);
			opacity: 0;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.sheet {
			animation: none;
		}
	}
	.handle {
		background: none;
		padding: 0.7rem 0 0.8rem;
		display: flex;
		justify-content: center;
		width: 100%;
		flex: none;
	}
	.handle span {
		width: 40px;
		height: 5px;
		border-radius: 5px;
		background: var(--line);
	}
	.scroll {
		overflow-y: auto;
		overscroll-behavior: contain;
	}
</style>
