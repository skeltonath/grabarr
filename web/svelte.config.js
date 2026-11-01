import adapter from '@sveltejs/adapter-static';

/** @type {import('@sveltejs/kit').Config} */
export default {
	kit: {
		// Single-page app: the Go server serves index.html for every unknown path.
		adapter: adapter({ fallback: 'index.html' })
	}
};
