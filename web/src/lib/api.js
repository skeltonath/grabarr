/**
 * Call the grabarr API and return the response's `data`. A 401 (the session
 * ran out while the page was open) starts sign-in again and comes back here.
 * Other failures throw an Error with the server's message.
 * @param {string} path  e.g. "/api/v1/jobs?limit=50"
 * @param {string} [method]
 * @param {any} [body]  sent as JSON
 */
export async function api(path, method = 'GET', body = undefined) {
	const init = { method };
	if (body !== undefined) {
		init.headers = { 'Content-Type': 'application/json' };
		init.body = JSON.stringify(body);
	}
	const res = await fetch(path, init);
	if (res.status === 401) {
		signIn();
		throw new Error('Signing in again');
	}
	if (res.status === 204) return null;
	const json = await res.json().catch(() => null);
	if (!res.ok || json?.success === false) {
		throw new Error(json?.error ?? `${res.status} ${res.statusText}`);
	}
	return json?.data ?? null;
}

/** Like api(), but also returns the pagination block list endpoints send. */
export async function apiPage(path) {
	const res = await fetch(path);
	if (res.status === 401) {
		signIn();
		throw new Error('Signing in again');
	}
	const json = await res.json().catch(() => null);
	if (!res.ok || json?.success === false) {
		throw new Error(json?.error ?? `${res.status} ${res.statusText}`);
	}
	return { data: json.data ?? [], pagination: json.pagination ?? null };
}

export function signIn() {
	location.href = '/auth/login?return_to=' + encodeURIComponent(location.pathname + location.search);
}

export async function signOut() {
	await fetch('/auth/logout', { method: 'POST' });
	location.href = '/';
}
