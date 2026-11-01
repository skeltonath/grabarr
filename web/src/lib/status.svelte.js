import { api } from './api.js';

/**
 * Service status shared by every page: gatekeeper resources and job counts,
 * polled while the tab is visible.
 */
export const status = $state({ resources: null, jobs: null, error: '' });

let timer;
async function refresh() {
	try {
		const data = await api('/api/v1/status');
		status.resources = data.resources;
		status.jobs = data.jobs;
		status.error = '';
	} catch (e) {
		status.error = String(e.message ?? e);
	}
}

export function startStatus() {
	refresh();
	clearInterval(timer);
	timer = setInterval(() => {
		if (!document.hidden) refresh();
	}, 10000);
	return () => clearInterval(timer);
}

/** The gatekeeper notice, or '' when nothing is holding copies back. */
export function holdBack(r) {
	if (!r) return '';
	if (r.cache_max_percent && r.cache_usage_percent >= r.cache_max_percent) {
		return `Cache disk is ${Math.round(r.cache_usage_percent)}% full. New copies wait until it drops below ${r.cache_max_percent}%.`;
	}
	return '';
}
