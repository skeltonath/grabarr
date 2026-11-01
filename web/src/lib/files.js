// The Files page's view of the tree: one status per file, counts per folder,
// filtering, and collapsing single-child folder chains.

/**
 * What a file looks like to a person, as opposed to the raw remote_files
 * status: a file back "on seedbox" because its copy failed is "failed".
 * @returns {'seedbox'|'queued'|'copying'|'copied'|'failed'|'ignored'}
 */
export function fileState(file) {
	const job = file.progress?.job_status;
	if (job === 'failed') return 'failed';
	switch (file.status) {
		case 'downloading':
			return 'copying';
		case 'downloaded':
			return 'copied';
		case 'queued':
			return job === 'running' ? 'copying' : 'queued';
		case 'ignored':
			return 'ignored';
		default:
			return 'seedbox';
	}
}

export const states = [
	{ id: 'seedbox', label: 'On seedbox' },
	{ id: 'queued', label: 'Queued' },
	{ id: 'copying', label: 'Copying' },
	{ id: 'copied', label: 'Copied' },
	{ id: 'failed', label: 'Failed' },
	{ id: 'ignored', label: 'Ignored' }
];

/** Every file under a node. */
export function filesUnder(node, out = []) {
	if (node.type === 'file') out.push(node);
	else for (const c of node.children ?? []) filesUnder(c, out);
	return out;
}

/**
 * Counts by state, plus total files and bytes, for any node. doneBytes counts
 * copied files and the part of copying ones already moved, so a folder's bar
 * moves while its one big file copies.
 */
export function tally(node) {
	const t = { files: 0, bytes: 0, wantedBytes: 0, doneBytes: 0, seedbox: 0, queued: 0, copying: 0, copied: 0, failed: 0, ignored: 0 };
	for (const f of filesUnder(node)) {
		const st = fileState(f);
		const b = f.size_bytes ?? 0;
		t.files++;
		t[st]++;
		t.bytes += b;
		if (st !== 'ignored') t.wantedBytes += b;
		if (st === 'copied') t.doneBytes += b;
		if (st === 'copying') t.doneBytes += f.progress?.transferred_bytes ?? 0;
	}
	return t;
}

/**
 * "6 of 10 copied · 1 copying · 3 queued". Ignored files don't count towards
 * the whole, since nobody means to copy them.
 */
export function summary(t) {
	const wanted = t.files - t.ignored;
	const parts = [];
	if (t.copied) parts.push(`${t.copied} of ${wanted} copied`);
	if (t.copying) parts.push(`${t.copying} copying`);
	if (t.queued) parts.push(`${t.queued} queued`);
	if (t.seedbox) parts.push(`${t.seedbox} on seedbox`);
	if (t.failed) parts.push(`${t.failed} failed`);
	if (t.ignored) parts.push(`${t.ignored} ignored`);
	return parts.join(' · ');
}

/**
 * The tree as rows to show: folders whose only child is a folder merge into
 * one row ("audiobooks / Project Hail Mary"), and a filter keeps a file when
 * it matches and a folder when anything inside it does.
 * @param {any} node  a folder
 * @param {(f: any) => boolean} keep
 * @returns {any[]}  folder rows carry {label, folder, path, children}, file rows {file}
 */
export function visibleChildren(node, keep) {
	const out = [];
	for (const child of node.children ?? []) {
		if (child.type === 'file') {
			if (keep(child)) out.push({ file: child, key: child.path });
			continue;
		}
		let folder = child;
		const names = [folder.name];
		while (folder.children?.length === 1 && folder.children[0].type === 'folder') {
			folder = folder.children[0];
			names.push(folder.name);
		}
		if (filesUnder(folder).some(keep)) {
			out.push({ label: names.join(' / '), folder, key: folder.path });
		}
	}
	// Folders first, then files, each by name.
	return out.sort((a, b) =>
		!!a.file === !!b.file
			? (a.label ?? a.file.name).localeCompare(b.label ?? b.file.name, undefined, { numeric: true })
			: a.file
				? 1
				: -1
	);
}
