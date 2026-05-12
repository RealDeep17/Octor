import av from '../../lib/av';
import { langPath } from '../../lib/i18n';

// ── Formatters ────────────────────────────────────────────────────────────
function fmtBytes(bytes) {
    if (!bytes || bytes <= 0) return null;
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(1024));
    return (bytes / Math.pow(1024, i)).toFixed(i >= 2 ? 1 : 0) + ' ' + units[i];
}
function fmtSpeed(bps) { const s = fmtBytes(bps); return s ? s + '/s' : null; }
function fmtETA(sec) {
    if (!sec || sec <= 0 || !isFinite(sec)) return null;
    if (sec < 60)   return `${Math.round(sec)}s`;
    if (sec < 3600) return `${Math.round(sec / 60)}m`;
    return `${Math.floor(sec / 3600)}h ${Math.round((sec % 3600) / 60)}m`;
}

// ── Per-row controller ────────────────────────────────────────────────────
function attachRow(item) {
    const resourceId = item.dataset.resourceId;
    const csrf       = item.dataset.csrf;
    if (!resourceId || !csrf) return null;

    const badge   = item.querySelector('[data-vault-progress-badge]');
    const bar     = item.querySelector('[data-vault-progress-bar]');
    const elSpeed = item.querySelector('[data-vault-stat-speed]');
    const elSize  = item.querySelector('[data-vault-stat-size]');
    const elEta   = item.querySelector('[data-vault-stat-eta]');
    const elPeers = item.querySelector('[data-vault-stat-peers]');

    // EMA alpha: 0.2 = heavy smoothing. Higher = react faster to changes.
    const EMA_ALPHA = 0.25;

    let emaSpeed     = 0;  // smoothed speed (bytes/s)
    let lastRemaining = 0;
    let pct          = 0;
    let peers        = 0;
    let state        = 'idle';
    let interpTimer  = null;
    let lastEventMs  = 0;

    function renderUI() {
        const speed = emaSpeed;
        const rem   = lastRemaining;
        const eta   = speed > 0 ? rem / speed : 0;

        if (bar)    bar.style.width = pct + '%';
        if (badge) {
            if (state === 'vaulted' || state === 'cached') {
                badge.innerHTML = `<span class="badge badge-sm bg-green-500/10 border-green-500/30 text-green-400">Saved</span>`;
            } else if (state === 'downloading' || speed > 0) {
                const p  = pct > 0 && pct < 100 ? `${pct}% · ` : '';
                const sp = speed > 0 ? fmtSpeed(speed) : null;
                badge.innerHTML = `<span class="badge badge-sm bg-w-cyan/10 border-w-cyan/30 text-w-cyan gap-1.5">
                    <span class="loading loading-dots loading-xs"></span>${p}${sp || 'Downloading'}
                </span>`;
            } else {
                badge.innerHTML = `<span class="badge badge-sm bg-w-muted/10 text-w-muted border-0">Pending</span>`;
            }
        }

        const show = (el, text) => { if (el) { if (text) { el.textContent = text; el.classList.remove('hidden'); } else el.classList.add('hidden'); } };
        show(elSpeed, speed > 0 ? '↓ ' + fmtSpeed(speed) : null);
        show(elSize,  rem > 0   ? fmtBytes(rem) + ' left' : null);
        show(elEta,   eta > 0   ? 'ETA ' + fmtETA(eta) : null);
        show(elPeers, peers > 0 ? '⇄ ' + peers + ' peers' : null);
    }

    // Interpolation tick every 500ms: reduce remaining, keep EMA speed stable.
    // This makes the display feel live between the seeder's 3s stat events.
    function startInterp() {
        if (interpTimer) return;
        const TICK = 500;
        interpTimer = setInterval(() => {
            if (emaSpeed > 0 && lastRemaining > 0) {
                lastRemaining = Math.max(0, lastRemaining - emaSpeed * TICK / 1000);
            }
            // Decay EMA slightly so it doesn't show stale high speed forever
            const msSinceEvent = Date.now() - lastEventMs;
            if (msSinceEvent > 8000) {
                emaSpeed *= 0.85;
            }
            renderUI();
        }, TICK);
    }

    function stopInterp() {
        if (interpTimer) { clearInterval(interpTimer); interpTimer = null; }
    }

    const url    = `${langPath(`/${resourceId}/status`)}?_csrf=${encodeURIComponent(csrf)}`;
    const source = new EventSource(url);

    source.onmessage = (e) => {
        let s;
        try { s = JSON.parse(e.data); } catch { return; }

        state = s.state || 'idle';
        pct   = Math.round(s.progress || 0);
        peers = s.seeders || 0;
        lastEventMs = Date.now();

        // Apply EMA to the raw speed from the seeder.
        // This is the key fix: instead of showing the raw (spiky) value, blend it.
        const rawSpeed = s.speed_bytes || 0;
        if (rawSpeed > 0) {
            emaSpeed = emaSpeed === 0
                ? rawSpeed
                : EMA_ALPHA * rawSpeed + (1 - EMA_ALPHA) * emaSpeed;
        } else if (state !== 'downloading') {
            emaSpeed = 0;
        }

        if (s.remaining_bytes > 0) {
            lastRemaining = s.remaining_bytes;
        }

        renderUI();

        if (state === 'vaulted' || state === 'cached') {
            stopInterp();
            source.close();
            return;
        }
        startInterp();
    };

    source.onerror = () => {
        // On SSE error decay speed gently, don't wipe it
        emaSpeed *= 0.5;
        renderUI();
    };

    return { source, cleanup: stopInterp };
}

// ── Init ──────────────────────────────────────────────────────────────────
av(async function () {
    const items = this.querySelectorAll('[data-vault-progress]');
    if (!items.length) return;

    const handles = [];
    items.forEach(item => {
        const h = attachRow(item);
        if (h) handles.push(h);
    });
    this._vaultProgressHandles = handles;
}, function () {
    if (this._vaultProgressHandles) {
        this._vaultProgressHandles.forEach(h => { h.source.close(); h.cleanup(); });
        this._vaultProgressHandles = null;
    }
});

export {};
