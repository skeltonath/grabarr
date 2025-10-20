// Grabarr Web UI JavaScript

class GrabarrDashboard {
    constructor() {
        this.apiBase = '/api/v1';
        this.refreshInterval = null;
        this.currentJobs = [];
        this.currentFilter = '';
        this.currentSearch = '';

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
            this.filterAndDisplayJobs();
        });

        // Search input
        const searchInput = document.getElementById('search-input');
        searchInput.addEventListener('input', (e) => {
            this.currentSearch = e.target.value.toLowerCase();
            this.filterAndDisplayJobs();
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
    }

    startAutoRefresh() {
        this.stopAutoRefresh(); // Clear existing interval
        this.refreshInterval = setInterval(() => {
            this.loadJobs();
        }, 3000); // Refresh every 3 seconds
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
            this.loadJobs()
        ]);
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
            const response = await fetch(`${this.apiBase}/jobs?limit=100`);
            const data = await response.json();

            if (data.success) {
                this.currentJobs = data.data || [];
                this.filterAndDisplayJobs();
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

        // Apply status filter
        if (this.currentFilter) {
            filteredJobs = filteredJobs.filter(job => job.status === this.currentFilter);
        }

        // Apply search filter
        if (this.currentSearch) {
            filteredJobs = filteredJobs.filter(job =>
                job.name.toLowerCase().includes(this.currentSearch) ||
                job.remote_path.toLowerCase().includes(this.currentSearch)
            );
        }

        this.displayJobs(filteredJobs);
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

        tbody.innerHTML = jobs.map(job => `
            <tr onclick="dashboard.showJobDetails(${job.id})" data-job-id="${job.id}">
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
        `).join('');
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