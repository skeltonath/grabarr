<script>
	import '$lib/house.css';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { status, startStatus, holdBack } from '$lib/status.svelte.js';

	let { children } = $props();

	const nav = [
		{ href: '/', label: 'Files' },
		{ href: '/jobs', label: 'Jobs' },
		{ href: '/settings', label: 'Settings' }
	];
	const notice = $derived(holdBack(status.resources));

	onMount(startStatus);
</script>

<header>
	<a class="brand" href="/">Grabarr</a>
	<nav>
		{#each nav as n (n.href)}
			<a href={n.href} aria-current={page.url.pathname === n.href ? 'page' : undefined}>{n.label}</a>
		{/each}
	</nav>
</header>

{#if notice}
	<div class="notice" role="status">{notice}</div>
{/if}

{@render children()}

<style>
	header {
		display: flex;
		align-items: baseline;
		gap: 1.1rem;
		padding: 1rem var(--gutter) 0.6rem;
	}
	@media (display-mode: standalone) {
		header {
			padding-top: calc(1rem + 12px);
		}
	}
	.brand {
		font-weight: 700;
		font-size: 1.2rem;
		letter-spacing: -0.02em;
		color: var(--text);
		text-decoration: none;
		margin-right: auto;
	}
	nav {
		display: flex;
		gap: 1.1rem;
	}
	nav a {
		color: var(--muted);
		text-decoration: none;
		padding: 0.4rem 0;
	}
	nav a[aria-current='page'] {
		color: var(--text);
	}
	.notice {
		margin: 0.2rem var(--gutter) 0.6rem;
		background: var(--surface);
		border: 1px solid var(--warm);
		border-radius: 12px;
		padding: 0.6rem 0.8rem;
		font-size: 0.95rem;
	}
	@media (min-width: 900px) {
		header {
			border-bottom: 1px solid var(--line);
			padding-bottom: 0.8rem;
			padding-left: 24px;
			padding-right: 24px;
		}
		.notice {
			margin: 0.8rem 24px 0;
		}
	}
</style>
