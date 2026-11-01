const units = ['B', 'KB', 'MB', 'GB', 'TB'];

/** 3.7 GB, 317 MB, 90 KB. */
export function size(bytes) {
	if (!bytes) return '0 B';
	let i = 0;
	let n = bytes;
	while (n >= 1024 && i < units.length - 1) {
		n /= 1024;
		i++;
	}
	return `${n >= 100 || i === 0 ? Math.round(n) : n.toFixed(1)} ${units[i]}`;
}

/** 58 MB/s. */
export function speed(bytesPerSec) {
	return bytesPerSec ? `${size(bytesPerSec)}/s` : '';
}

/** 38 s, 16 min, 2 h 5 min. */
export function duration(seconds) {
	const s = Math.max(0, Math.round(seconds));
	if (s < 60) return `${s} s`;
	const m = Math.round(s / 60);
	if (m < 60) return `${m} min`;
	const h = Math.floor(m / 60);
	return m % 60 ? `${h} h ${m % 60} min` : `${h} h`;
}

/** Time left for a transfer, from what's done and the current speed. */
export function timeLeft(total, done, bytesPerSec) {
	if (!bytesPerSec || !total || done >= total) return '';
	return `${duration((total - done) / bytesPerSec)} left`;
}

/** "just now", "4 min ago", "3 h ago", "2 d ago". */
export function ago(iso, now = Date.now()) {
	if (!iso) return '';
	const s = (now - new Date(iso).getTime()) / 1000;
	if (s < 45) return 'just now';
	if (s < 3600) return `${Math.round(s / 60)} min ago`;
	if (s < 86400 * 2) return `${Math.round(s / 3600)} h ago`;
	return `${Math.round(s / 86400)} d ago`;
}

/** A full local date and time, for tooltips and detail views. */
export function when(iso) {
	return iso ? new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }) : '';
}

/** dp-movies → movies, dp-audiobooks → audiobooks. */
export function category(c) {
	return (c ?? '').replace(/^dp-/, '');
}

/** Percent of a whole, rounded, never NaN. */
export function pct(part, whole) {
	return whole ? Math.round((part / whole) * 100) : 0;
}

/**
 * The container sees millions' /mnt/user and /mnt/cache as /unraid/user and
 * /unraid/cache (docker-compose.yml); show the paths as they are on the host.
 */
export function hostPath(p) {
	return (p ?? '').replace(/^\/unraid\//, '/mnt/');
}
