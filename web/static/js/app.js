// Grabarr Web UI JavaScript

class GrabarrDashboard {
    constructor() {
        this.apiBase = '/api/v1';
        this.refreshInterval = null;
        this.currentJobs = [];
        this.currentFilter = '';
        this.currentSearch = '';
        this.expandedGroups = new Set(); // Track which groups are expanded

        // Pagination state (Jobs tab)
        this.currentPage = 1;
        this.pageSize = 50;
        this.totalJobs = 0;
        this.totalPages = 0;

        // Active tab
        this.activeTab = 'seedbox';

        // Seedbox state
        this.seedboxFiles = [];
        this.seedboxFilter = '';
        this.seedboxSearch = '';
        this.seedboxPage = 1;
        this.seedboxPageSize = 100;
        this.seedboxTotal = 0;
        this.seedboxTotalPages = 0;
        this.expandedFolders = new Set(); // all folders expanded by default on first load
        this.firstSeedboxLoad = true;

        this.init();
    }

    init() {
        this.initTheme();
        this.setupEventListeners();
        this.startAutoRefresh();
        this.loadDashboard();
    }

    initTheme() {
        // Load theme from localStorage or default to light
        const savedTheme = localStorage.getItem('theme') || 'light';
        document.documentElement.setAttribute('data-theme', savedTheme);
        this.updateThemeIcon(savedTheme);
    }

    updateThemeIcon(theme) {
        const icon = document.getElementById('theme-icon');
        icon.textContent = theme === 'dark' ? '☀️' : '🌙';
    }

    toggleTheme() {
        const currentTheme = document.documentElement.getAttribute('data-theme');
        const newTheme = currentTheme === 'dark' ? 'light' : 'dark';

        document.documentElement.setAttribute('data-theme', newTheme);
        localStorage.setItem('theme', newTheme);
        this.updateThemeIcon(newTheme);
    }

    setupEventListeners() {
        // Theme toggle
        document.getElementById('theme-toggle').addEventListener('click', () => {
            this.toggleTheme();
        });

        // Refresh button
        document.getElementById('refresh-btn').addEventListener('click', () => {
            this.loadDashboard();
        });

        // Auto-refresh toggle
        const autoRefreshCheckbox = document.getElementById('auto-refresh');
        autoRefreshCheckbox.addEventListener('change', (e) => {
            if (e.target.checked) {
                this.startAutoRefresh();
            } else {
                this.stopAutoRefresh();
            }
        });

        // Status filter
        document.getElementById('status-filter').addEventListener('change', (e) => {
            this.currentFilter = e.target.value;
            this.currentPage = 1; // Reset to first page on filter change
            this.loadJobs();
        });

        // Search input (client-side filtering within current page)
        const searchInput = document.getElementById('search-input');
        searchInput.addEventListener('input', (e) => {
            this.currentSearch = e.target.value.toLowerCase();
            this.filterAndDisplayJobs();
        });

        // Pagination controls
        document.getElementById('prev-page-btn')?.addEventListener('click', () => {
            this.prevPage();
        });
        document.getElementById('next-page-btn')?.addEventListener('click', () => {
            this.nextPage();
        });

        // Job modal controls
        document.getElementById('modal-close').addEventListener('click', () => {
            this.closeModal('job-modal');
        });
        document.getElementById('modal-close-btn').addEventListener('click', () => {
            this.closeModal('job-modal');
        });

        // Modal background click
        document.getElementById('job-modal').addEventListener('click', (e) => {
            if (e.target.id === 'job-modal') {
                this.closeModal('job-modal');
            }
        });

        // Modal action buttons
        document.getElementById('modal-cancel-btn').addEventListener('click', () => {
            this.cancelJob();
        });
        document.getElementById('modal-retry-btn').addEventListener('click', () => {
            this.retryJob();
        });
        document.getElementById('modal-delete-btn').addEventListener('click', () => {
            this.deleteJob();
        });

        // Confirmation modal controls
        document.getElementById('confirm-cancel-btn').addEventListener('click', () => {
            this.closeConfirmModal(false);
        });
        document.getElementById('confirm-ok-btn').addEventListener('click', () => {
            this.closeConfirmModal(true);
        });

        // Confirmation modal background click
        document.getElementById('confirm-modal').addEventListener('click', (e) => {
            if (e.target.id === 'confirm-modal') {
                this.closeConfirmModal(false);
            }
        });

        // Seedbox controls
        document.getElementById('seedbox-status-filter')?.addEventListener('change', (e) => {
            this.seedboxFilter = e.target.value;
            this.seedboxPage = 1;
            this.loadSeedbox();
        });
        document.getElementById('seedbox-search-input')?.addEventListener('input', (e) => {
            this.seedboxSearch = e.target.value.toLowerCase();
            this.filterAndDisplaySeedbox();
        });
        document.getElementById('seedbox-prev-page-btn')?.addEventListener('click', () => {
            if (this.seedboxPage > 1) { this.seedboxPage--; this.loadSeedbox(); }
        });
        document.getElementById('seedbox-next-page-btn')?.addEventListener('click', () => {
            if (this.seedboxPage < this.seedboxTotalPages) { this.seedboxPage++; this.loadSeedbox(); }
        });
        document.getElementById('scan-now-btn')?.addEventListener('click', () => {
            this.triggerScan();
        });

        // Remote file modal close
        document.getElementById('remote-file-modal-close')?.addEventListener('click', () => {
            this.closeModal('remote-file-modal');
        });
        document.getElementById('remote-file-modal-close-btn')?.addEventListener('click', () => {
            this.closeModal('remote-file-modal');
        });
        document.getElementById('remote-file-modal')?.addEventListener('click', (e) => {
            if (e.target.id === 'remote-file-modal') this.closeModal('remote-file-modal');
        });
    }

    switchTab(tab) {
        this.activeTab = tab;
        document.querySelectorAll('.tab-btn').forEach(btn => btn.classList.remove('active'));
        document.querySelectorAll('.tab-content').forEach(c => c.classList.add('hidden'));
        document.getElementById(`tab-${tab}`)?.classList.add('active');
        document.getElementById(`tab-content-${tab}`)?.classList.remove('hidden');

        if (tab === 'seedbox') {
            this.loadSeedbox();
            this.loadSyncStatus();
        }
    }

    startAutoRefresh() {
        this.stopAutoRefresh(); // Clear existing interval
        this.refreshInterval = setInterval(() => {
            if (this.activeTab === 'seedbox') {
                this.loadSeedbox();
                this.loadSyncStatus();
            } else {
                this.loadJobs();
            }
        }, 5000); // Refresh every 5 seconds
    }

    stopAutoRefresh() {
        if (this.refreshInterval) {
            clearInterval(this.refreshInterval);
            this.refreshInterval = null;
        }
    }

    async loadDashboard() {
        await Promise.all([
            this.loadSystemStatus(),
            this.loadSeedbox(),
            this.loadSyncStatus(),
        ]);
    }

    async loadSeedbox() {
        try {
            const offset = (this.seedboxPage - 1) * this.seedboxPageSize;
            let url = `${this.apiBase}/remote-files?limit=${this.seedboxPageSize}&offset=${offset}`;
            if (this.seedboxFilter) url += `&status=${this.seedboxFilter}`;

            const response = await fetch(url);
            const data = await response.json();

            if (data.success) {
                this.seedboxFiles = data.data || [];
                if (data.pagination) {
                    this.seedboxTotal = data.pagination.total;
                    this.seedboxTotalPages = data.pagination.total_pages;
                }
                this.filterAndDisplaySeedbox();
                this.updateSeedboxPagination();
            }
        } catch (error) {
            console.error('Error loading seedbox files:', error);
        }
    }

    async loadSyncStatus() {
        try {
            const response = await fetch(`${this.apiBase}/sync/status`);
            const data = await response.json();
            if (data.success && data.data) {
                this.renderSyncStatus(data.data);
            }
        } catch (error) {
            console.error('Error loading sync status:', error);
        }
    }

    renderSyncStatus(status) {
        const bar = document.getElementById('sync-status-text');
        if (!bar) return;

        if (!status.enabled) {
            bar.textContent = 'Scanner disabled';
            return;
        }

        let text = '';
        if (status.scan_in_flight) {
            text = 'Scanning...';
        } else if (status.last_scan_at) {
            const ago = this.timeAgo(new Date(status.last_scan_at));
            text = `Last scan: ${ago} · ${status.files_found} files`;
        } else {
            text = 'No scan yet';
        }
        if (status.error) text += ` · Error: ${status.error}`;
        bar.textContent = text;
    }

    filterAndDisplaySeedbox() {
        let files = this.seedboxFiles;
        // Status filter is applied server-side; only search is client-side
        if (this.seedboxSearch) {
            files = files.filter(f =>
                f.name.toLowerCase().includes(this.seedboxSearch) ||
                f.remote_path.toLowerCase().includes(this.seedboxSearch)
            );
        }
        this.displaySeedbox(files);
    }

    // Build a two-level tree: top-level dirs → files (files at root level are ungrouped).
    buildFileTree(files) {
        const folders = new Map(); // dirName → { files: [{file, relPath}], totalSize }
        const rootFiles = [];

        for (const f of files) {
            const watchedPath = f.watched_path || '';
            const rel = f.remote_path.startsWith(watchedPath)
                ? f.remote_path.slice(watchedPath.length)
                : f.name;

            const slashIdx = rel.indexOf('/');
            if (slashIdx === -1) {
                // File sits directly in the watched dir — show ungrouped
                rootFiles.push({ file: f, relPath: rel });
            } else {
                const dirName = rel.slice(0, slashIdx);
                if (!folders.has(dirName)) {
                    folders.set(dirName, { files: [], totalSize: 0 });
                }
                const entry = folders.get(dirName);
                entry.files.push({ file: f, relPath: rel.slice(slashIdx + 1) });
                entry.totalSize += f.size || 0;
            }
        }

        return { folders, rootFiles };
    }

    displaySeedbox(files) {
        const tbody = document.getElementById('seedbox-tbody');
        if (!tbody) return;

        if (!files || files.length === 0) {
            tbody.innerHTML = '<tr class="loading-row"><td colspan="4">No files found</td></tr>';
            return;
        }

        // On first load, expand all folders automatically
        if (this.firstSeedboxLoad) {
            const { folders } = this.buildFileTree(files);
            for (const dirName of folders.keys()) {
                this.expandedFolders.add(dirName);
            }
            this.firstSeedboxLoad = false;
        }

        const { folders, rootFiles } = this.buildFileTree(files);

        let html = '';

        for (const [dirName, { files: dirFiles, totalSize }] of folders) {
            const isExpanded = this.expandedFolders.has(dirName);
            html += this.renderFolderRow(dirName, dirFiles, totalSize, isExpanded);
            if (isExpanded) {
                for (const { file, relPath } of dirFiles) {
                    html += this.renderSeedboxRow(file, relPath, true);
                }
            }
        }

        for (const { file, relPath } of rootFiles) {
            html += this.renderSeedboxRow(file, relPath, false);
        }

        tbody.innerHTML = html;
    }

    renderFolderRow(dirName, dirFiles, totalSize, isExpanded) {
        const icon = isExpanded ? '▼' : '▶';
        const size = this.formatBytes(totalSize);
        const fileCount = dirFiles.length;

        // Aggregate status summary
        const statusCounts = {};
        for (const { file } of dirFiles) {
            statusCounts[file.status] = (statusCounts[file.status] || 0) + 1;
        }
        const statusSummary = Object.entries(statusCounts)
            .map(([s, n]) => `<span class="status-badge seedbox-status-${s}">${n} ${s.replace('_', ' ')}</span>`)
            .join(' ');

        // "Download Folder" queues all on_seedbox files in this dir
        const queueableIds = dirFiles
            .filter(({ file }) => file.status === 'on_seedbox')
            .map(({ file }) => file.id);
        const dlBtn = queueableIds.length > 0
            ? `<button class="btn btn-sm btn-primary" onclick="event.stopPropagation(); dashboard.downloadFolder(${JSON.stringify(queueableIds)})">Download All (${queueableIds.length})</button>`
            : '';

        return `
            <tr class="group-header-row" onclick="dashboard.toggleFolder(${JSON.stringify(dirName)})">
                <td colspan="4">
                    <div class="group-header">
                        <span class="group-expand-icon">${icon}</span>
                        <span class="group-path">${this.escapeHtml(dirName)}</span>
                        <span class="group-count">${fileCount} file${fileCount !== 1 ? 's' : ''} · ${size}</span>
                        <span class="folder-status-summary">${statusSummary}</span>
                        <span class="folder-actions" onclick="event.stopPropagation()">${dlBtn}</span>
                    </div>
                </td>
            </tr>
        `;
    }

    toggleFolder(dirName) {
        if (this.expandedFolders.has(dirName)) {
            this.expandedFolders.delete(dirName);
        } else {
            this.expandedFolders.add(dirName);
        }
        this.filterAndDisplaySeedbox();
    }

    async downloadFolder(ids) {
        if (!ids || ids.length === 0) return;
        this.showLoading(true);
        try {
            let queued = 0;
            for (const id of ids) {
                const response = await fetch(`${this.apiBase}/remote-files/${id}/queue`, { method: 'POST' });
                const data = await response.json();
                if (data.success) queued++;
            }
            this.showToast(`${queued} download${queued !== 1 ? 's' : ''} queued`, 'success');
            this.loadSeedbox();
        } catch (error) {
            this.showError('Failed to queue folder downloads');
        } finally {
            this.showLoading(false);
        }
    }

    renderSeedboxRow(f, relPath, isGrouped) {
        const statusBadge = `<span class="status-badge seedbox-status-${f.status}">${f.status.replace('_', ' ')}</span>`;
        const size = this.formatBytes(f.size || 0);
        const rowClass = isGrouped ? 'grouped-job-row' : '';
        const displayName = relPath || f.name;

        let actions = '';
        switch (f.status) {
            case 'on_seedbox':
                actions = `
                    <button class="btn btn-sm btn-primary" onclick="event.stopPropagation(); dashboard.queueRemoteFile(${f.id})">Download</button>
                    <button class="btn btn-sm btn-secondary" onclick="event.stopPropagation(); dashboard.ignoreRemoteFile(${f.id})">Ignore</button>
                `;
                break;
            case 'queued':
            case 'downloading':
                actions = `<span class="muted-text">In progress</span>`;
                break;
            case 'downloaded':
                actions = `<button class="btn btn-sm btn-secondary" onclick="event.stopPropagation(); dashboard.redownloadRemoteFile(${f.id})">Re-download</button>`;
                break;
            case 'ignored':
                actions = `<button class="btn btn-sm btn-secondary" onclick="event.stopPropagation(); dashboard.restoreRemoteFile(${f.id})">Restore</button>`;
                break;
        }

        return `
            <tr class="${rowClass}" onclick="dashboard.showRemoteFileDetails(${f.id})" data-file-id="${f.id}">
                <td><div class="job-name" title="${this.escapeHtml(f.remote_path)}">${this.escapeHtml(displayName)}</div></td>
                <td>${size}</td>
                <td>${statusBadge}</td>
                <td onclick="event.stopPropagation()">${actions}</td>
            </tr>
        `;
    }

    updateSeedboxPagination() {
        const prevBtn = document.getElementById('seedbox-prev-page-btn');
        const nextBtn = document.getElementById('seedbox-next-page-btn');
        const pageInfo = document.getElementById('seedbox-page-info');
        if (prevBtn && nextBtn && pageInfo) {
            prevBtn.disabled = this.seedboxPage <= 1;
            nextBtn.disabled = this.seedboxPage >= this.seedboxTotalPages;
            pageInfo.textContent = `Page ${this.seedboxPage} of ${this.seedboxTotalPages} (${this.seedboxTotal} files)`;
        }
    }

    async triggerScan() {
        const btn = document.getElementById('scan-now-btn');
        if (btn) { btn.disabled = true; btn.textContent = 'Scanning...'; }
        try {
            const response = await fetch(`${this.apiBase}/sync/scan`, { method: 'POST' });
            const data = await response.json();
            if (data.success) {
                this.showToast('Scan started', 'success');
                setTimeout(() => { this.loadSeedbox(); this.loadSyncStatus(); }, 2000);
            } else {
                this.showError(data.error || 'Scan failed');
            }
        } catch (error) {
            this.showError('Failed to trigger scan');
        } finally {
            if (btn) { btn.disabled = false; btn.textContent = 'Scan Now'; }
        }
    }

    async queueRemoteFile(id) {
        try {
            this.showLoading(true);
            const response = await fetch(`${this.apiBase}/remote-files/${id}/queue`, { method: 'POST' });
            const data = await response.json();
            if (data.success) {
                this.showToast('Download queued', 'success');
                this.loadSeedbox();
            } else {
                this.showError(data.error || 'Failed to queue download');
            }
        } catch (error) {
            this.showError('Failed to queue download');
        } finally {
            this.showLoading(false);
        }
    }

    async ignoreRemoteFile(id) {
        try {
            const response = await fetch(`${this.apiBase}/remote-files/${id}/ignore`, { method: 'POST' });
            const data = await response.json();
            if (data.success) {
                this.loadSeedbox();
            } else {
                this.showError(data.error || 'Failed to ignore file');
            }
        } catch (error) {
            this.showError('Failed to ignore file');
        }
    }

    async restoreRemoteFile(id) {
        try {
            const response = await fetch(`${this.apiBase}/remote-files/${id}/restore`, { method: 'POST' });
            const data = await response.json();
            if (data.success) {
                this.loadSeedbox();
            } else {
                this.showError(data.error || 'Failed to restore file');
            }
        } catch (error) {
            this.showError('Failed to restore file');
        }
    }

    async redownloadRemoteFile(id) {
        try {
            await fetch(`${this.apiBase}/remote-files/${id}/restore`, { method: 'POST' });
            const response = await fetch(`${this.apiBase}/remote-files/${id}/queue`, { method: 'POST' });
            const data = await response.json();
            if (data.success) {
                this.showToast('Re-download queued', 'success');
                this.loadSeedbox();
            } else {
                this.showError(data.error || 'Failed to re-download');
            }
        } catch (error) {
            this.showError('Failed to re-download');
        }
    }

    showRemoteFileDetails(id) {
        const f = this.seedboxFiles.find(f => f.id === id);
        if (!f) return;

        document.getElementById('remote-file-modal-title').textContent = f.name;
        document.getElementById('remote-file-modal-body').innerHTML = `
            <div class="job-detail">
                <div class="detail-group">
                    <div class="detail-label">Remote Path:</div>
                    <div class="detail-value">${this.escapeHtml(f.remote_path)}</div>
                </div>
                <div class="detail-group">
                    <div class="detail-label">Size:</div>
                    <div class="detail-value">${this.formatBytes(f.size || 0)}</div>
                </div>
                <div class="detail-group">
                    <div class="detail-label">Extension:</div>
                    <div class="detail-value">${this.escapeHtml(f.extension || '-')}</div>
                </div>
                <div class="detail-group">
                    <div class="detail-label">Status:</div>
                    <div class="detail-value"><span class="status-badge seedbox-status-${f.status}">${f.status.replace('_', ' ')}</span></div>
                </div>
                <div class="detail-group">
                    <div class="detail-label">Watched Path:</div>
                    <div class="detail-value">${this.escapeHtml(f.watched_path || '-')}</div>
                </div>
                ${f.job_id ? `<div class="detail-group"><div class="detail-label">Linked Job:</div><div class="detail-value">#${f.job_id}</div></div>` : ''}
                <div class="detail-group">
                    <div class="detail-label">First Seen:</div>
                    <div class="detail-value">${this.formatDateTime(f.first_seen_at)}</div>
                </div>
                <div class="detail-group">
                    <div class="detail-label">Last Seen:</div>
                    <div class="detail-value">${this.formatDateTime(f.last_seen_at)}</div>
                </div>
            </div>
        `;
        this.openModal('remote-file-modal');
    }

    timeAgo(date) {
        const seconds = Math.floor((new Date() - date) / 1000);
        if (seconds < 60) return `${seconds}s ago`;
        if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
        return `${Math.floor(seconds / 3600)}h ago`;
    }

    async loadSystemStatus() {
        try {
            const response = await fetch(`${this.apiBase}/health`);
            const data = await response.json();

            if (data.success) {
                this.updateSystemStatus('connected', 'Connected');
            } else {
                this.updateSystemStatus('disconnected', 'Error');
            }
        } catch (error) {
            console.error('Error loading system status:', error);
            this.updateSystemStatus('disconnected', 'Disconnected');
        }

        this.updateLastUpdated();
    }

    async loadJobs() {
        try {
            const offset = (this.currentPage - 1) * this.pageSize;
            let url = `${this.apiBase}/jobs?limit=${this.pageSize}&offset=${offset}`;

            // Add status filter to API request
            if (this.currentFilter) {
                url += `&status=${this.currentFilter}`;
            }

            const response = await fetch(url);
            const data = await response.json();

            if (data.success) {
                this.currentJobs = data.data || [];

                // Update pagination state from response
                if (data.pagination) {
                    this.totalJobs = data.pagination.total;
                    this.totalPages = data.pagination.total_pages;
                }

                this.filterAndDisplayJobs();
                this.updatePaginationControls();
            }
        } catch (error) {
            console.error('Error loading jobs:', error);
            this.showError('Failed to load jobs');
        }
    }

    updateSystemStatus(status, text) {
        const indicator = document.getElementById('status-indicator');
        const statusText = document.getElementById('status-text');

        indicator.className = `status-indicator ${status === 'connected' ? '' : 'disconnected'}`;
        statusText.textContent = text;
    }

    updateLastUpdated() {
        const now = new Date();
        document.getElementById('last-updated').textContent = now.toLocaleTimeString();
    }

    filterAndDisplayJobs() {
        let filteredJobs = this.currentJobs;

        // Apply client-side search filter (searches within current page only)
        if (this.currentSearch) {
            filteredJobs = filteredJobs.filter(job =>
                job.name.toLowerCase().includes(this.currentSearch) ||
                job.remote_path.toLowerCase().includes(this.currentSearch)
            );
        }

        this.displayJobs(filteredJobs);
    }

    nextPage() {
        if (this.currentPage < this.totalPages) {
            this.currentPage++;
            this.loadJobs();
        }
    }

    prevPage() {
        if (this.currentPage > 1) {
            this.currentPage--;
            this.loadJobs();
        }
    }

    goToPage(page) {
        if (page >= 1 && page <= this.totalPages) {
            this.currentPage = page;
            this.loadJobs();
        }
    }

    updatePaginationControls() {
        const prevBtn = document.getElementById('prev-page-btn');
        const nextBtn = document.getElementById('next-page-btn');
        const pageInfo = document.getElementById('page-info');

        if (prevBtn && nextBtn && pageInfo) {
            // Update button states
            prevBtn.disabled = this.currentPage <= 1;
            nextBtn.disabled = this.currentPage >= this.totalPages;

            // Update page info text
            pageInfo.textContent = `Page ${this.currentPage} of ${this.totalPages} (${this.totalJobs} total jobs)`;
        }
    }

    groupJobsByPath(jobs) {
        const groups = new Map();
        const ungrouped = [];

        for (const job of jobs) {
            // Group by torrent name if available
            const torrentName = job.metadata?.torrent_name;

            if (torrentName) {
                // Has torrent name - can be grouped
                if (!groups.has(torrentName)) {
                    groups.set(torrentName, []);
                }
                groups.get(torrentName).push(job);
            } else {
                // No torrent name - ungrouped
                ungrouped.push(job);
            }
        }

        // Filter out groups with only 1 job (treat as ungrouped)
        const finalGroups = [];
        for (const [torrentName, groupJobs] of groups) {
            if (groupJobs.length > 1) {
                // Sort jobs within group by ID ascending (file order)
                groupJobs.sort((a, b) => a.id - b.id);

                // Find the most recent created_at in this group
                const maxCreatedAt = Math.max(...groupJobs.map(j => new Date(j.created_at)));

                finalGroups.push({
                    torrentName,
                    jobs: groupJobs,
                    sortKey: maxCreatedAt
                });
            } else {
                ungrouped.push(...groupJobs);
            }
        }

        // Sort ungrouped jobs by created_at descending
        ungrouped.sort((a, b) => new Date(b.created_at) - new Date(a.created_at));

        // Sort groups by most recent job in each group (descending)
        finalGroups.sort((a, b) => b.sortKey - a.sortKey);

        // Create display items array that interleaves groups and ungrouped jobs
        const displayItems = [];
        let groupIndex = 0;
        let ungroupedIndex = 0;

        while (groupIndex < finalGroups.length || ungroupedIndex < ungrouped.length) {
            const nextGroup = finalGroups[groupIndex];
            const nextUngrouped = ungrouped[ungroupedIndex];

            if (!nextGroup && nextUngrouped) {
                // No more groups, add remaining ungrouped
                displayItems.push({ type: 'job', job: nextUngrouped });
                ungroupedIndex++;
            } else if (!nextUngrouped && nextGroup) {
                // No more ungrouped, add remaining groups
                displayItems.push({ type: 'group', group: nextGroup });
                groupIndex++;
            } else {
                // Compare timestamps to determine order
                const groupTime = nextGroup.sortKey;
                const ungroupedTime = new Date(nextUngrouped.created_at).getTime();

                if (groupTime >= ungroupedTime) {
                    displayItems.push({ type: 'group', group: nextGroup });
                    groupIndex++;
                } else {
                    displayItems.push({ type: 'job', job: nextUngrouped });
                    ungroupedIndex++;
                }
            }
        }

        return { displayItems };
    }

    toggleGroup(groupPath) {
        if (this.expandedGroups.has(groupPath)) {
            this.expandedGroups.delete(groupPath);
        } else {
            this.expandedGroups.add(groupPath);
        }

        // Re-render to show/hide group items
        this.filterAndDisplayJobs();
    }

    toggleGroupFromElement(element) {
        const groupPath = element.getAttribute('data-group-path');
        if (groupPath) {
            this.toggleGroup(groupPath);
        }
    }

    displayJobs(jobs) {
        const tbody = document.getElementById('jobs-tbody');

        if (jobs.length === 0) {
            tbody.innerHTML = `
                <tr class="loading-row">
                    <td colspan="8">No jobs found</td>
                </tr>
            `;
            return;
        }

        const { displayItems } = this.groupJobsByPath(jobs);

        let html = '';

        // Render display items (groups and ungrouped jobs in sorted order)
        for (const item of displayItems) {
            if (item.type === 'group') {
                const { torrentName, jobs: groupJobs } = item.group;
                const isExpanded = this.expandedGroups.has(torrentName);
                const expandIcon = isExpanded ? '▼' : '▶';

                // Group header row
                html += `
                    <tr class="group-header-row" data-group-path="${this.escapeHtml(torrentName)}" onclick="dashboard.toggleGroupFromElement(this)">
                        <td colspan="8">
                            <div class="group-header">
                                <span class="group-expand-icon">${expandIcon}</span>
                                <span class="group-path">${this.escapeHtml(torrentName)}</span>
                                <span class="group-count">(${groupJobs.length} files)</span>
                            </div>
                        </td>
                    </tr>
                `;

                // Group job rows (only if expanded)
                if (isExpanded) {
                    for (const job of groupJobs) {
                        html += this.renderJobRow(job, true);
                    }
                }
            } else {
                // Ungrouped job
                html += this.renderJobRow(item.job, false);
            }
        }

        tbody.innerHTML = html;
    }

    renderJobRow(job, isGrouped) {
        const rowClass = isGrouped ? 'grouped-job-row' : '';
        return `
            <tr class="${rowClass}" onclick="dashboard.showJobDetails(${job.id})" data-job-id="${job.id}">
                <td>${job.id}</td>
                <td>
                    <div class="job-name" title="${this.escapeHtml(job.name)}">
                        ${this.truncateText(job.name, 30)}
                    </div>
                </td>
                <td>
                    <span class="status-badge status-${job.status}">
                        ${job.status}
                    </span>
                </td>
                <td>
                    ${this.renderProgress(job.progress)}
                </td>
                <td>${this.formatSpeed(job.progress?.transfer_speed || 0)}</td>
                <td>${this.formatETA(job.progress?.eta)}</td>
                <td>${this.formatDate(job.created_at)}</td>
                <td>
                    <button class="btn btn-sm" onclick="event.stopPropagation(); dashboard.showJobDetails(${job.id})">
                        View
                    </button>
                </td>
            </tr>
        `;
    }

    renderProgress(progress) {
        if (!progress || progress.percentage === 0) {
            return '<div class="progress-bar"><div class="progress-fill" style="width: 0%"></div></div>';
        }

        const percentage = Math.min(100, Math.max(0, progress.percentage || 0));
        return `
            <div class="progress-bar">
                <div class="progress-fill" style="width: ${percentage}%"></div>
            </div>
            <div style="font-size: 0.75rem; margin-top: 2px;">${percentage.toFixed(1)}%</div>
        `;
    }

    async showJobDetails(jobId) {
        try {
            this.showLoading(true);

            const response = await fetch(`${this.apiBase}/jobs/${jobId}`);
            const data = await response.json();

            if (data.success && data.data) {
                this.renderJobModal(data.data);
                this.openModal('job-modal');
            } else {
                this.showError('Failed to load job details');
            }
        } catch (error) {
            console.error('Error loading job details:', error);
            this.showError('Failed to load job details');
        } finally {
            this.showLoading(false);
        }
    }

    renderJobModal(job) {
        document.getElementById('modal-title').textContent = `Job #${job.id}: ${job.name}`;

        const modalBody = document.getElementById('modal-body');
        modalBody.innerHTML = `
            <div class="job-detail">
                <div class="detail-group">
                    <div class="detail-label">ID:</div>
                    <div class="detail-value">${job.id}</div>
                </div>

                <div class="detail-group">
                    <div class="detail-label">Name:</div>
                    <div class="detail-value">${this.escapeHtml(job.name)}</div>
                </div>

                <div class="detail-group">
                    <div class="detail-label">Status:</div>
                    <div class="detail-value">
                        <span class="status-badge status-${job.status}">${job.status}</span>
                    </div>
                </div>

                <div class="detail-group">
                    <div class="detail-label">Remote Path:</div>
                    <div class="detail-value">${this.escapeHtml(job.remote_path)}</div>
                </div>

                <div class="detail-group">
                    <div class="detail-label">Local Path:</div>
                    <div class="detail-value">${this.escapeHtml(job.local_path || 'Auto')}</div>
                </div>

                <div class="detail-group">
                    <div class="detail-label">Priority:</div>
                    <div class="detail-value">${job.priority}</div>
                </div>

                <div class="detail-group">
                    <div class="detail-label">Retries:</div>
                    <div class="detail-value">${job.retries}/${job.max_retries}</div>
                </div>

                ${job.error_message ? `
                <div class="detail-group">
                    <div class="detail-label">Error:</div>
                    <div class="detail-value" style="color: var(--status-failed);">${this.escapeHtml(job.error_message)}</div>
                </div>
                ` : ''}

                <div class="detail-group">
                    <div class="detail-label">Progress:</div>
                    <div class="detail-value">
                        <div class="progress-detail">
                            <div class="progress-bar-large">
                                <div class="progress-fill-large" style="width: ${job.progress?.percentage || 0}%"></div>
                            </div>
                            <div style="display: flex; justify-content: space-between; font-size: 0.875rem; margin-top: 0.5rem;">
                                <span>${(job.progress?.percentage || 0).toFixed(1)}%</span>
                                <span>${this.formatBytes(job.progress?.transferred_bytes || 0)} / ${this.formatBytes(job.progress?.total_bytes || 0)}</span>
                            </div>
                            ${job.progress?.transfer_speed ? `
                            <div style="font-size: 0.875rem;">
                                Speed: ${this.formatSpeed(job.progress.transfer_speed)}
                            </div>
                            ` : ''}
                            ${job.progress?.eta ? `
                            <div style="font-size: 0.875rem;">
                                ETA: ${this.formatETA(job.progress.eta)}
                            </div>
                            ` : ''}
                        </div>
                    </div>
                </div>

                <div class="detail-group">
                    <div class="detail-label">Created:</div>
                    <div class="detail-value">${this.formatDateTime(job.created_at)}</div>
                </div>

                ${job.started_at ? `
                <div class="detail-group">
                    <div class="detail-label">Started:</div>
                    <div class="detail-value">${this.formatDateTime(job.started_at)}</div>
                </div>
                ` : ''}

                ${job.completed_at ? `
                <div class="detail-group">
                    <div class="detail-label">Completed:</div>
                    <div class="detail-value">${this.formatDateTime(job.completed_at)}</div>
                </div>
                ` : ''}
            </div>
        `;

        // Show appropriate action buttons
        const cancelBtn = document.getElementById('modal-cancel-btn');
        const retryBtn = document.getElementById('modal-retry-btn');
        const deleteBtn = document.getElementById('modal-delete-btn');

        // Hide all buttons first
        cancelBtn.style.display = 'none';
        retryBtn.style.display = 'none';
        deleteBtn.style.display = 'none';

        if (job.status === 'running' || job.status === 'queued') {
            cancelBtn.style.display = 'block';
        } else if (job.status === 'failed') {
            retryBtn.style.display = 'block';
            deleteBtn.style.display = 'block';
        } else {
            deleteBtn.style.display = 'block';
        }

        // Store job ID for actions
        this.currentModalJobId = job.id;
    }

    async cancelJob() {
        if (!this.currentModalJobId) return;

        const confirmed = await this.showConfirm(
            'Cancel Job',
            'Are you sure you want to cancel this job?'
        );

        if (!confirmed) return;

        const cancelBtn = document.getElementById('modal-cancel-btn');

        try {
            this.showLoading(true);

            const response = await fetch(`${this.apiBase}/jobs/${this.currentModalJobId}/cancel`, {
                method: 'POST'
            });

            const data = await response.json();

            if (data.success) {
                this.closeModal('job-modal');
                this.loadDashboard();
                this.showSuccess('Job cancelled successfully', cancelBtn);
            } else {
                this.showError('Failed to cancel job');
            }
        } catch (error) {
            console.error('Error cancelling job:', error);
            this.showError('Failed to cancel job');
        } finally {
            this.showLoading(false);
        }
    }

    async retryJob() {
        if (!this.currentModalJobId) return;

        const confirmed = await this.showConfirm(
            'Retry Job',
            'Are you sure you want to retry this job?'
        );

        if (!confirmed) return;

        const retryBtn = document.getElementById('modal-retry-btn');

        try {
            this.showLoading(true);

            const response = await fetch(`${this.apiBase}/jobs/${this.currentModalJobId}/retry`, {
                method: 'POST'
            });

            const data = await response.json();

            if (data.success) {
                this.closeModal('job-modal');
                this.loadDashboard();
                this.showSuccess('Job retried successfully', retryBtn);
            } else {
                this.showError(data.error || 'Failed to retry job');
            }
        } catch (error) {
            console.error('Error retrying job:', error);
            this.showError('Failed to retry job');
        } finally {
            this.showLoading(false);
        }
    }

    async deleteJob() {
        if (!this.currentModalJobId) return;

        const confirmed = await this.showConfirm(
            'Delete Job',
            'Are you sure you want to delete this job? This action cannot be undone.'
        );

        if (!confirmed) return;

        const deleteBtn = document.getElementById('modal-delete-btn');

        try {
            this.showLoading(true);

            const response = await fetch(`${this.apiBase}/jobs/${this.currentModalJobId}`, {
                method: 'DELETE'
            });

            const data = await response.json();

            if (data.success) {
                this.closeModal('job-modal');
                this.loadDashboard();
                this.showSuccess('Job deleted successfully', deleteBtn);
            } else {
                this.showError('Failed to delete job');
            }
        } catch (error) {
            console.error('Error deleting job:', error);
            this.showError('Failed to delete job');
        } finally {
            this.showLoading(false);
        }
    }

    openModal(modalId) {
        document.getElementById(modalId).classList.add('active');
        document.body.style.overflow = 'hidden';
    }

    closeModal(modalId) {
        document.getElementById(modalId).classList.remove('active');
        document.body.style.overflow = '';
        if (modalId === 'job-modal') {
            this.currentModalJobId = null;
        }
    }

    showLoading(show) {
        const overlay = document.getElementById('loading-overlay');
        overlay.style.display = show ? 'flex' : 'none';
    }

    showError(message) {
        this.showToast(message, 'error');
    }

    showSuccess(message, button) {
        if (button) {
            this.showButtonSuccess(button);
        }
    }

    showToast(message, type = 'error') {
        const container = document.getElementById('toast-container');

        // Create toast element
        const toast = document.createElement('div');
        toast.className = 'toast';

        // Icon based on type
        const icon = type === 'error' ? '⚠️' : '✓';

        toast.innerHTML = `
            <span class="toast-icon">${icon}</span>
            <span class="toast-message">${this.escapeHtml(message)}</span>
            <button class="toast-close" onclick="this.parentElement.remove()">×</button>
        `;

        container.appendChild(toast);

        // Auto-remove after 5 seconds
        setTimeout(() => {
            if (toast.parentElement) {
                toast.classList.add('removing');
                setTimeout(() => {
                    if (toast.parentElement) {
                        toast.remove();
                    }
                }, 300); // Match animation duration
            }
        }, 5000);
    }

    showButtonSuccess(button) {
        // Save original content and state
        const originalText = button.innerHTML;
        const originalDisabled = button.disabled;

        // Update button to success state
        button.innerHTML = '✓';
        button.classList.add('btn-success-state');
        button.disabled = true;

        // Restore after 2 seconds
        setTimeout(() => {
            button.innerHTML = originalText;
            button.classList.remove('btn-success-state');
            button.disabled = originalDisabled;
        }, 2000);
    }

    showConfirm(title, message) {
        return new Promise((resolve) => {
            // Set the modal content
            document.getElementById('confirm-title').textContent = title;
            document.getElementById('confirm-body').textContent = message;

            // Store the resolve function
            this.confirmResolve = resolve;

            // Show the modal
            this.openModal('confirm-modal');
        });
    }

    closeConfirmModal(confirmed) {
        this.closeModal('confirm-modal');
        if (this.confirmResolve) {
            this.confirmResolve(confirmed);
            this.confirmResolve = null;
        }
    }

    // Utility functions
    formatBytes(bytes) {
        if (bytes === 0) return '0 B';
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }

    formatSpeed(bytesPerSecond) {
        return this.formatBytes(bytesPerSecond) + '/s';
    }

    formatETA(eta) {
        if (!eta) return '-';

        // ETA is a time.Time string from Go - parse it and calculate duration from now
        const etaDate = new Date(eta);
        const now = new Date();
        const diffMs = etaDate - now;

        if (diffMs <= 0) return 'Soon';

        const seconds = Math.floor(diffMs / 1000);

        if (seconds < 60) return `${seconds}s`;
        if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;

        const hours = Math.floor(seconds / 3600);
        const minutes = Math.floor((seconds % 3600) / 60);
        return `${hours}h ${minutes}m`;
    }

    formatDate(dateString) {
        const date = new Date(dateString);
        return date.toLocaleDateString();
    }

    formatDateTime(dateString) {
        const date = new Date(dateString);
        return date.toLocaleString();
    }

    truncateText(text, maxLength) {
        if (text.length <= maxLength) return text;
        return text.substring(0, maxLength - 3) + '...';
    }

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
}

// Initialize dashboard when page loads
let dashboard;
document.addEventListener('DOMContentLoaded', () => {
    dashboard = new GrabarrDashboard();
});

// Handle page visibility changes to pause/resume auto-refresh
document.addEventListener('visibilitychange', () => {
    if (dashboard) {
        const autoRefreshEnabled = document.getElementById('auto-refresh').checked;
        if (document.hidden) {
            dashboard.stopAutoRefresh();
        } else if (autoRefreshEnabled) {
            dashboard.startAutoRefresh();
            dashboard.loadDashboard(); // Immediate refresh when page becomes visible
        }
    }
});