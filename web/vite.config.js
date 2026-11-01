import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// In dev, Vite serves the UI on :5173 and proxies API and auth calls to the Go
// server (`make preview`) on :8080.
const api = 'http://localhost:8080';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		proxy: {
			'/api': api,
			'/auth': api
		}
	}
});
