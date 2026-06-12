import av from '../../lib/av';
import { langPath } from '../../lib/i18n';

// rgba colors mirror the o-primary / o-primary / green-500 tokens at low alpha
const TINTS = {
    caching:  'rgba(0, 206, 201, 0.10)',
    cached:   'rgba(0, 206, 201, 0.06)',
    vaulting: 'rgba(108, 92, 231, 0.12)',
    waiting:  'rgba(108, 92, 231, 0.06)',
    vaulted:  'rgba(34, 197, 94, 0.08)',
    idle:     'rgba(0, 206, 201, 0.06)',
};

// Floor for caching/vaulting widths so 0–1 % progress is still visible.
const MIN_VISIBLE_PCT = 2;
// Below this fill width, the percent indicator can't fit inside the filled
// portion (translateX(-100%) would clip it off the left edge of the row), so
// it flips to the right side of the gradient edge instead.
const FLIP_PCT = 10;

const BADGE_CONFIG = {
    idle: {
        classes: 'badge badge-sm bg-base-200/50 border-o-line/30 text-o-muted gap-1.5',
        icon: '<span class="loading loading-dots loading-xs"></span>',
    },
    caching: {
        classes: 'badge badge-sm bg-o-primary/10 border-o-primary/30 text-o-primary gap-1.5',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5M16.5 12 12 16.5m0 0L7.5 12m4.5 4.5V3" /></svg>',
    },
    cached: {
        classes: 'badge badge-sm bg-o-primary/10 border-o-primary/30 text-o-primary gap-1.5',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3 h-3"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75 11.25 15 15 9.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" /></svg>',
    },
    vaulting: {
        // no leading icon: the row gradient + first-cell pulse already signal progress
        classes: 'badge badge-sm bg-o-primary/10 border-o-primary/30 text-o-accent',
        icon: '',
    },
    waiting: {
        classes: 'badge badge-sm bg-base-200/50 border-o-line/30 text-o-muted gap-1.5',
        icon: '<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-3.5 h-3.5 animate-pulse"><path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" /></svg>',
    },
    vaulted: {
        // status column = torrent state only; frozen-ness lives on the VP cell
        classes: 'badge badge-sm bg-green-500/10 border-green-500/30 text-green-400',
        icon: '',
    },
};

function ensureIndicator(row) {
    let el = row.querySelector('[data-vault-progress-pct]');
    if (el) return el;
    const host = row.cells && row.cells[0];
    if (!host) return null;
    el = document.createElement('span');
    el.dataset.vaultProgressPct = '';
    el.className = 'vault-progress-pct hidden';
    host.appendChild(el);
    return el;
}

function applyRowFill(row, status) {
    const color = TINTS[status.state] || TINTS.idle;
    const rawPct = Math.round(status.progress || 0);
    let pct;
    let showPct = false;
    switch (status.state) {
        case 'caching':
        case 'vaulting':
            pct = Math.max(MIN_VISIBLE_PCT, rawPct);
            showPct = true;
            break;
        case 'cached':
            pct = 100;
            break;
        case 'waiting':
        case 'vaulted':
        default:
            pct = 0;
            break;
    }
    if (pct > 0) {
        row.style.backgroundImage = `linear-gradient(to right, ${color} ${pct}%, transparent ${pct}%)`;
    } else {
        row.style.backgroundImage = 'none';
    }

    const indicator = ensureIndicator(row);
    if (!indicator) return;
    if (showPct) {
        indicator.textContent = `${rawPct}%`;
        indicator.style.left = `${pct}%`;
        // Below FLIP_PCT the fill is too narrow to host the indicator on its
        // left side (translateX(-100%) would clip past the row's left edge),
        // so flip it to the right side of the gradient edge.
        indicator.classList.toggle('vault-progress-pct--right', pct < FLIP_PCT);
        indicator.classList.remove('hidden');
    } else {
        indicator.classList.add('hidden');
    }
}

function settleVaultedIcon(row) {
    const icon = row.querySelector('[data-vault-progress-icon]');
    if (icon) icon.classList.remove('vault-pulse');
}

function getShortErrorName(errorStr) {
    if (!errorStr) return 'Error';
    let s = errorStr.replace(/^Error:\s*/i, '').replace(/\(retrying\.\.\.\)/i, '').trim();
    if (s.includes('502')) return 'Error 502';
    if (s.includes('503')) return 'Error 503';
    if (s.includes('500')) return 'Error 500';
    if (s.includes('403')) return 'Error 403';
    if (s.includes('404')) return 'Error 404';
    let parts = s.split(':').map(p => p.trim()).filter(Boolean);
    if (parts.length > 0) {
        let candidate = parts[0];
        if (candidate.toLowerCase().includes('fetch torrent')) return 'Fetch Failed';
        if (candidate.toLowerCase().includes('store file')) return 'Store Failed';
        if (candidate.toLowerCase().includes('generate file hash')) return 'Hash Failed';
        if (candidate.length > 20) return candidate.substring(0, 18) + '...';
        return candidate.charAt(0).toUpperCase() + candidate.slice(1);
    }
    return 'Error';
}

function cleanErrorMessage(errorStr) {
    if (!errorStr) return '';
    let s = errorStr;
    s = s.replace(/,?\s*url=https?:\/\/[^\s,]+/gi, '');
    s = s.replace(/https?:\/\/[^\s]+/gi, '[link]');
    s = s.replace(/\s+/g, ' ').trim();
    return s;
}

function renderBadge(status, savedLabel) {
    const hasError = status.detail && status.detail.startsWith('Error:');
    if (hasError) {
        const shortName = getShortErrorName(status.detail);
        const cleanedReason = cleanErrorMessage(status.detail).replace(/"/g, '&quot;');
        const classes = 'badge badge-sm bg-error/10 border-error/30 text-error gap-1.5 font-semibold cursor-help';
        const icon = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="w-3.5 h-3.5"><path fill-rule="evenodd" d="M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0Zm-8-5a.75.75 0 0 1 .75.75v4.5a.75.75 0 0 1-1.5 0v-4.5A.75.75 0 0 1 10 5Zm0 10a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z" clip-rule="evenodd" /></svg>';
        return `<span class="${classes}" title="${cleanedReason}">${icon} ${shortName}</span>`;
    }
    const config = BADGE_CONFIG[status.state] || BADGE_CONFIG.idle;
    // For the vaulted state we override the server label ('В Vault'/'Vaulted') with
    // the vault-page label ('Сохранён'/'Saved') passed via data-vault-saved-label.
    let label = (status.state === 'vaulted' && savedLabel) ? savedLabel : (status.label || '');
    if ((status.state === 'vaulting' || status.state === 'caching') && status.progress > 0) {
        label = `${label} ${Math.round(status.progress)}%`;
    }
    // status.label is a server-translated i18n string (closed set of state keys);
    // safe to interpolate as HTML.
    const inner = [config.icon, label].filter(Boolean).join(' ');
    return `<span class="${config.classes}">${inner}</span>`;
}

function attachRow(row) {
    const resourceId = row.dataset.resourceId;
    const csrf = row.dataset.csrf || window._CSRF;
    if (!resourceId || !csrf) return null;

    const savedLabel = row.dataset.vaultSavedLabel || '';
    const badge = row.querySelector('[data-vault-progress-badge]');

    const url = `${langPath(`/${resourceId}/status`)}?_csrf=${encodeURIComponent(csrf)}&active=1`;
    const source = new EventSource(url);

    source.onmessage = (e) => {
        let status;
        try {
            status = JSON.parse(e.data);
        } catch (err) {
            return;
        }
        applyRowFill(row, status);
        if (badge) badge.innerHTML = renderBadge(status, savedLabel);

        const retryForm = row.querySelector('[data-vault-retry-form]');

        // Update stats line
        const statsLine = row.querySelector('[data-vault-stats-line]');
        if (statsLine) {
            const speedEl = statsLine.querySelector('[data-vault-stat-speed]');
            const sizeEl = statsLine.querySelector('[data-vault-stat-size]');
            const etaEl = statsLine.querySelector('[data-vault-stat-eta]');
            const peersEl = statsLine.querySelector('[data-vault-stat-peers]');
            if (status.state === 'caching' || status.state === 'vaulting') {
                const hasError = status.detail && status.detail.startsWith('Error:');

                if (retryForm) {
                    if (hasError) {
                        retryForm.classList.remove('hidden');
                    } else {
                        retryForm.classList.add('hidden');
                    }
                }

                if (!hasError && status.speed_bytes > 0 && speedEl) {
                    speedEl.textContent = `${formatBytes(status.speed_bytes)}/s`;
                    speedEl.classList.remove('hidden');
                } else if (speedEl) speedEl.classList.add('hidden');

                if (status.completed_str && status.total_str && sizeEl) {
                    sizeEl.textContent = `${status.completed_str} of ${status.total_str}`;
                    sizeEl.classList.remove('hidden');
                } else if (sizeEl) sizeEl.classList.add('hidden');

                if (!hasError && status.eta_seconds > 0 && etaEl) {
                    etaEl.textContent = `ETA ${formatETA(status.eta_seconds)}`;
                    etaEl.classList.remove('hidden');
                } else if (etaEl) etaEl.classList.add('hidden');

                if (!hasError && status.seeders > 0 && peersEl) {
                    peersEl.textContent = `${status.seeders} seed${status.seeders === 1 ? '' : 's'}`;
                    peersEl.classList.remove('hidden');
                } else if (peersEl) peersEl.classList.add('hidden');
                statsLine.classList.remove('hidden');
            } else {
                statsLine.classList.add('hidden');
                if (retryForm) retryForm.classList.add('hidden');
            }
        }

        // Update progress bar
        const progressBar = row.querySelector('[data-vault-progress-bar]');
        if (progressBar) {
            progressBar.style.width = `${status.progress}%`;
        }

        if (status.state === 'vaulted') {
            settleVaultedIcon(row);
            if (statsLine) statsLine.classList.add('hidden');
            const progressBarWrap = row.querySelector('[data-vault-progress-bar-wrap]');
            if (progressBarWrap) progressBarWrap.classList.add('hidden');
            if (retryForm) retryForm.classList.add('hidden');
            source.close();
        }
    };
    return source;
}

function formatBytes(v) {
    if (v <= 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let value = v;
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
        value /= 1024;
        unit++;
    }
    return unit === 0 ? `${Math.round(value)} ${units[unit]}` : `${value.toFixed(1)} ${units[unit]}`;
}

function formatETA(seconds) {
    if (seconds <= 0) return '';
    if (seconds >= 86400) return `${Math.floor(seconds / 86400)}d ${Math.floor((seconds % 86400) / 3600)}h`;
    if (seconds >= 3600) return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
    if (seconds >= 60) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
    return `${seconds}s`;
}

av(async function () {
    const root = this;

    // Bind interactive client-side search filtering
    const searchInput = root.querySelector('#vault-search-form input[name="q"]');
    const statusSelect = root.querySelector('#vault-status-filter');
    const items = root.querySelectorAll('.vault-item');
    const emptyState = root.querySelector('#vault-search-empty');

    const filterItems = () => {
        const query = searchInput ? searchInput.value.trim().toLowerCase() : '';
        const status = statusSelect ? statusSelect.value : 'all';
        let visibleCount = 0;

        items.forEach((item) => {
            const titleLink = item.querySelector('a[data-async-target="main"], .font-medium, .vault-name-link');
            const titleText = titleLink ? titleLink.textContent.trim().toLowerCase() : '';
            const matchesQuery = !query || titleText.includes(query);

            const rowStatus = item.getAttribute('data-vault-status') || 'vaulting';
            const matchesStatus = status === 'all' || rowStatus === status;

            if (matchesQuery && matchesStatus) {
                item.classList.remove('hidden');
                visibleCount++;
            } else {
                item.classList.add('hidden');
            }
        });

        if (emptyState) {
            if (visibleCount === 0 && items.length > 0) {
                emptyState.classList.remove('hidden');
            } else {
                emptyState.classList.add('hidden');
            }
        }
    };

    if (searchInput) {
        searchInput.addEventListener('input', filterItems);
        searchInput.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') {
                e.preventDefault();
            }
        });
    }

    if (statusSelect) {
        statusSelect.addEventListener('change', filterItems);
    }

    // Trigger filter immediately on load
    filterItems();

    const rows = root.querySelectorAll('[data-vault-progress]');
    if (!rows.length) return;

    const sources = [];
    rows.forEach((row) => {
        const s = attachRow(row);
        if (s) sources.push(s);
    });
    root._vaultProgressSources = sources;
}, function () {
    const root = this;
    if (root._vaultProgressSources) {
        root._vaultProgressSources.forEach((s) => s.close());
        root._vaultProgressSources = null;
    }
});

export {};
