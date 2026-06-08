import { useRef, useEffect, useState, useMemo, useCallback } from 'preact/hooks';
import { rebindAsync } from '../../async';
import { initProgressLog } from '../../progressLog';
import { parseStreamName, extractInfoHash, extractFileIdx } from '../stream';
import { extractLanguages, LANG_MAP } from '../lang';
import { loadPrefs, savePrefs } from '../prefs';
import { chipClass } from './discoverUtils';
import { t, tf } from '../i18n';

export function StreamModal({ modal, onClose, onEpisodeSelect, onStreamClick, onBackToEpisodes, onSeasonChange, hasCustomAddons, onSetupAddons, onRetryStreams, onLoadMore, userStatuses, watchlistIds, onToggleWatched, onRate, onToggleWatchlist, stremioSettings = {} }) {
    const dialogRef = useRef(null);

    useEffect(() => {
        const dialog = dialogRef.current;
        if (!dialog) return;
        if (modal) {
            if (!dialog.open) dialog.showModal();
        } else {
            dialog.close();
        }
    }, [modal]);

    // Rebind async link handlers after Preact renders new <a data-async-target> elements
    useEffect(() => {
        if (modal && dialogRef.current) rebindAsync(dialogRef.current);
    }, [modal]);

    // Handle close via backdrop or Escape
    const handleClose = useCallback(() => {
        onClose();
    }, [onClose]);

    if (!modal) return null;

    return (
        <dialog ref={dialogRef} class="modal" onClose={handleClose}>
            <div class="modal-box bg-w-card border border-w-line/50 rounded-2xl max-w-2xl max-h-[calc(100dvh-2rem)] flex flex-col overflow-hidden p-0 text-left">
                <div class="flex justify-between items-center shrink-0 px-2 sm:px-6 pt-2 sm:pt-4 pb-1 sm:pb-2">
                    {onBackToEpisodes && (modal.view === 'streams' || modal.view === 'loading') ? (
                        <button
                            class="btn btn-sm btn-ghost text-w-muted hover:text-w-cyan gap-1 px-2"
                            onClick={onBackToEpisodes}
                        >
                            <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <path d="M15 18l-6-6 6-6"/>
                            </svg>
                            {t('discover.episodes')}
                        </button>
                    ) : <div />}
                    <button
                        class="btn btn-sm btn-circle btn-ghost text-w-muted hover:text-base-content"
                        onClick={handleClose}
                    >
                        &#10005;
                    </button>
                </div>
                <div class="overflow-y-auto px-3 sm:px-6 pb-4 sm:pb-6">
                    <ModalBody modal={modal} onClose={handleClose} onEpisodeSelect={onEpisodeSelect} onStreamClick={onStreamClick} onSeasonChange={onSeasonChange} hasCustomAddons={hasCustomAddons} onSetupAddons={onSetupAddons} onRetryStreams={onRetryStreams} onLoadMore={onLoadMore} userStatuses={userStatuses} watchlistIds={watchlistIds} onToggleWatched={onToggleWatched} onRate={onRate} onToggleWatchlist={onToggleWatchlist} stremioSettings={stremioSettings} />
                </div>
            </div>
            <form method="dialog" class="modal-backdrop">
                <button>close</button>
            </form>
        </dialog>
    );
}

function ModalBody({ modal, onClose, onEpisodeSelect, onStreamClick, onSeasonChange, hasCustomAddons, onSetupAddons, onRetryStreams, onLoadMore, userStatuses, watchlistIds, onToggleWatched, onRate, onToggleWatchlist, stremioSettings }) {
    const videoId = modal.metaId || modal.itemId;
    const videoType = modal.itemType;
    const isImdb = videoId && videoId.startsWith('tt') && !videoId.includes(':');
    const status = isImdb && videoType && userStatuses ? userStatuses[videoId] : null;
    const inWatchlist = !!(isImdb && watchlistIds && watchlistIds.has && watchlistIds.has(videoId));
    const statusButtons = isImdb && videoType ? (
        <WatchedRateButtons
            videoId={videoId}
            videoType={videoType}
            watched={status?.watched || false}
            rating={status?.rating || 0}
            inWatchlist={inWatchlist}
            onToggleWatched={onToggleWatched}
            onRate={onRate}
            onToggleWatchlist={onToggleWatchlist}
        />
    ) : null;
    const headerMeta = { year: modal.year || modal.releaseInfo, imdbRating: modal.imdbRating, description: modal.description };

    if (modal.view === 'loading') {
        return (
            <div>
                <ModalHeader title={modal.title} poster={modal.poster} subtitle={modal.subtitle} extra={statusButtons} {...headerMeta} />
                <p class="text-w-muted text-sm text-center py-6">{modal.subtitle || t('discover.loading')}</p>
            </div>
        );
    }

    if (modal.view === 'fetching') {
        return <FetchingView modal={modal} statusButtons={statusButtons} headerMeta={headerMeta} />;
    }

    if (modal.view === 'progress') {
        return <ProgressView logUrl={modal.logUrl} title={modal.title} poster={modal.poster} fileIdx={modal.fileIdx} />;
    }

    if (modal.view === 'episodes') {
        return <EpisodePicker key={modal._seasonKey} modal={modal} onEpisodeSelect={onEpisodeSelect} defaultSeason={modal.defaultSeason} onSeasonChange={onSeasonChange} statusButtons={statusButtons} headerMeta={headerMeta} />;
    }

    if (modal.view === 'streams') {
        return <StreamContent modal={modal} onStreamClick={onStreamClick} hasCustomAddons={hasCustomAddons} onSetupAddons={onSetupAddons} onRetryStreams={onRetryStreams} onLoadMore={onLoadMore} statusButtons={statusButtons} headerMeta={headerMeta} stremioSettings={stremioSettings} />;
    }

    return null;
}

function WatchedRateButtons({ videoId, videoType, watched, rating, inWatchlist, onToggleWatched, onRate, onToggleWatchlist }) {
    const handleWatched = useCallback((e) => {
        e.stopPropagation();
        if (onToggleWatched) onToggleWatched({ id: videoId, type: videoType });
    }, [videoId, videoType, onToggleWatched]);

    const handleRate = useCallback((e) => {
        e.stopPropagation();
        if (onRate) onRate({ id: videoId, type: videoType });
    }, [videoId, videoType, onRate]);

    const handleWatchlist = useCallback((e) => {
        e.stopPropagation();
        if (onToggleWatchlist) onToggleWatchlist({ id: videoId, type: videoType });
    }, [videoId, videoType, onToggleWatchlist]);

    // Mobile bumps the join group to btn-sm + larger icons for comfortable
    // touch targets (~36px tall, close to the 44px iOS guideline given the
    // inner icon's hit-box). Desktop stays btn-xs to keep the modal header
    // dense alongside the title and metadata.
    return (
        <div class="join mt-2 mb-1">
            {watched ? (
                <button type="button" onClick={handleWatched}
                    class="btn btn-ghost btn-sm sm:btn-xs join-item border border-green-500/20 text-green-400 hover:bg-green-500/10 whitespace-nowrap"
                    title={t('discover.unmarkWatched')}
                    aria-label={t('discover.unmarkWatched')}>
                    <svg class="w-5 h-5 sm:w-4 sm:h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                    <span class="hidden sm:inline">{t('discover.watched')}</span>
                </button>
            ) : (
                <button type="button" onClick={handleWatched}
                    class="btn btn-ghost btn-sm sm:btn-xs join-item border border-w-line text-w-sub hover:border-green-500/40 hover:text-green-400 whitespace-nowrap"
                    title={t('discover.markWatched')}
                    aria-label={t('discover.markWatched')}>
                    <svg class="w-5 h-5 sm:w-4 sm:h-4" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M2.036 12.322a1.012 1.012 0 0 1 0-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178Z" />
                        <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z" />
                    </svg>
                    <span class="hidden sm:inline">{t('discover.watched')}</span>
                </button>
            )}
            {rating > 0 ? (
                <button type="button" onClick={handleRate}
                    class="btn btn-ghost btn-sm sm:btn-xs join-item border border-yellow-500/20 text-yellow-400 hover:bg-yellow-500/10 whitespace-nowrap"
                    title={t('discover.changeRating')}
                    aria-label={t('discover.changeRating')}>
                    <svg class="w-5 h-5 sm:w-4 sm:h-4" viewBox="0 0 24 24" fill="currentColor"><path fill-rule="evenodd" d="M10.788 3.21c.448-1.077 1.976-1.077 2.424 0l2.082 5.006 5.404.434c1.164.093 1.636 1.545.749 2.305l-4.117 3.527 1.257 5.273c.271 1.136-.964 2.033-1.96 1.425L12 18.354 7.373 21.18c-.996.608-2.231-.29-1.96-1.425l1.257-5.273-4.117-3.527c-.887-.76-.415-2.212.749-2.305l5.404-.434 2.082-5.005Z" clip-rule="evenodd" /></svg>
                    {rating}
                </button>
            ) : (
                <button type="button" onClick={handleRate}
                    class="btn btn-ghost btn-sm sm:btn-xs join-item border border-w-line text-w-sub hover:border-yellow-500/40 hover:text-yellow-400 whitespace-nowrap"
                    title={t('discover.rate')}
                    aria-label={t('discover.rate')}>
                    <svg class="w-5 h-5 sm:w-4 sm:h-4" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M11.48 3.499a.562.562 0 0 1 1.04 0l2.125 5.111a.563.563 0 0 0 .475.345l5.518.442c.499.04.701.663.321.988l-4.204 3.602a.563.563 0 0 0-.182.557l1.285 5.385a.562.562 0 0 1-.84.61l-4.725-2.885a.562.562 0 0 0-.586 0L6.982 20.54a.562.562 0 0 1-.84-.61l1.285-5.386a.562.562 0 0 0-.182-.557l-4.204-3.602a.562.562 0 0 1 .321-.988l5.518-.442a.563.563 0 0 0 .475-.345L11.48 3.5Z" />
                    </svg>
                    <span class="hidden sm:inline">{t('discover.rate')}</span>
                </button>
            )}
            {onToggleWatchlist && (
                inWatchlist ? (
                    <button type="button" onClick={handleWatchlist}
                        class="btn btn-ghost btn-sm sm:btn-xs join-item border border-w-pink/30 text-w-pinkL hover:bg-w-pink/10 hover:border-w-pink/40 whitespace-nowrap"
                        title={t('discover.removeFromWatchlist')}
                        aria-label={t('discover.removeFromWatchlist')}>
                        <svg class="w-5 h-5 sm:w-4 sm:h-4" viewBox="0 0 24 24" fill="currentColor"><path d="M11.645 20.91l-.007-.003-.022-.012a15.247 15.247 0 0 1-.383-.218 25.18 25.18 0 0 1-4.244-3.17C4.688 15.36 2.25 12.174 2.25 8.25 2.25 5.322 4.714 3 7.688 3A5.5 5.5 0 0 1 12 5.052 5.5 5.5 0 0 1 16.313 3c2.973 0 5.437 2.322 5.437 5.25 0 3.925-2.438 7.111-4.739 9.256a25.175 25.175 0 0 1-4.244 3.17 15.247 15.247 0 0 1-.383.219l-.022.012-.007.004-.003.001a.752.752 0 0 1-.704 0l-.003-.001Z"/></svg>
                        <span class="hidden sm:inline">{t('discover.watchlist.label')}</span>
                    </button>
                ) : (
                    <button type="button" onClick={handleWatchlist}
                        class="btn btn-ghost btn-sm sm:btn-xs join-item border border-w-line text-w-sub hover:border-w-pink/40 hover:text-w-pinkL whitespace-nowrap"
                        title={t('discover.addToWatchlist')}
                        aria-label={t('discover.addToWatchlist')}>
                        <svg class="w-5 h-5 sm:w-4 sm:h-4" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round">
                            <path d="M21 8.25c0-2.485-2.099-4.5-4.688-4.5-1.935 0-3.597 1.126-4.312 2.733-.715-1.607-2.377-2.733-4.313-2.733C5.1 3.75 3 5.765 3 8.25c0 7.22 9 12 9 12s9-4.78 9-12Z" />
                        </svg>
                        <span class="hidden sm:inline">{t('discover.watchlist.label')}</span>
                    </button>
                )
            )}
        </div>
    );
}

function ModalHeader({ title, poster, subtitle, extra, afterDescription, year, imdbRating, description }) {
    const [imgError, setImgError] = useState(false);

    return (
        <div class="flex gap-3 sm:gap-5 mb-4">
            <div class="shrink-0 w-[100px] sm:w-[140px] aspect-[2/3] rounded-xl overflow-hidden border border-w-line/30 shadow-lg relative">
                <div class="absolute inset-0 bg-gradient-to-br from-w-purple/20 via-w-pink/10 to-w-cyan/15 text-w-purpleL/60 flex items-center justify-center">
                    <div class="text-center font-bold text-sm p-3 line-clamp-3 drop-shadow-sm">
                        {title || t('discover.unknown')}
                    </div>
                </div>
                {poster && !imgError && (
                    <img
                        src={poster}
                        alt={title || ''}
                        class="absolute inset-0 w-full h-full object-cover"
                        onError={() => setImgError(true)}
                    />
                )}
            </div>
            <div class="flex flex-col justify-center min-w-0">
                <h3 class="font-bold text-lg line-clamp-2">{title || t('discover.unknown')}</h3>
                {(year || imdbRating) && (
                    <div class="flex flex-wrap items-center gap-2 mt-1 text-sm text-w-sub">
                        {year && <span>{year}</span>}
                        {imdbRating && (
                            <span class="flex items-center gap-1 text-yellow-400">
                                <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor"><path fill-rule="evenodd" d="M10.788 3.21c.448-1.077 1.976-1.077 2.424 0l2.082 5.006 5.404.434c1.164.093 1.636 1.545.749 2.305l-4.117 3.527 1.257 5.273c.271 1.136-.964 2.033-1.96 1.425L12 18.354 7.373 21.18c-.996.608-2.231-.29-1.96-1.425l1.257-5.273-4.117-3.527c-.887-.76-.415-2.212.749-2.305l5.404-.434 2.082-5.005Z" clip-rule="evenodd" /></svg>
                                {parseFloat(imdbRating).toFixed(1)}
                            </span>
                        )}
                    </div>
                )}
                {extra}
                {description && <p class="text-sm text-w-sub leading-relaxed mt-1 line-clamp-3">{description}</p>}
                {afterDescription}
                {subtitle && <p class="text-sm text-w-muted mt-1">{subtitle}</p>}
            </div>
        </div>
    );
}

// --- Progress View ---

function ProgressView({ logUrl, title, poster, fileIdx }) {
    const containerRef = useRef(null);

    useEffect(() => {
        if (!logUrl || !containerRef.current) return;
        const form = containerRef.current.querySelector('form');
        if (!form) return;

        const sdk = initProgressLog(form);
        return () => sdk.destroy();
    }, [logUrl]);

    return (
        <div>
            <ModalHeader title={title} poster={poster} subtitle={t('discover.preparingResource')} />
            <div ref={containerRef}>
                {logUrl ? (
                    <form class="progress-alert" data-async-progress-log={logUrl} data-async-target="main">
                        {fileIdx != null && <input type="hidden" name="file-idx" value={fileIdx} />}
                        <div class="log-target"></div>
                    </form>
                ) : (
                    <div class="text-center py-4">
                        <span class="loading loading-spinner loading-md text-w-cyan"></span>
                    </div>
                )}
            </div>
        </div>
    );
}

// --- Fetching View (per-addon progress) ---

function FetchingView({ modal, statusButtons, headerMeta }) {
    const { title, poster, addons } = modal;
    const doneCount = addons.filter(a => a.status !== 'fetching').length;
    const subtitle = tf('discover.fetchingStreams', doneCount, addons.length);

    return (
        <div>
            <ModalHeader title={title} poster={poster} subtitle={subtitle} extra={statusButtons} {...headerMeta} />
            <div class="flex flex-col gap-2 py-2">
                {addons.map((addon, i) => (
                    <div key={i} class="flex items-center gap-3 px-3 py-2 rounded-lg border border-w-line/50">
                        {addon.status === 'fetching' && (
                            <span class="loading loading-spinner loading-xs text-w-cyan flex-shrink-0"></span>
                        )}
                        {addon.status === 'done' && (
                            <svg class="w-4 h-4 text-green-500 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                                <path d="M20 6L9 17l-5-5"/>
                            </svg>
                        )}
                        {addon.status === 'error' && (
                            <svg class="w-4 h-4 text-red-400 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                                <path d="M18 6L6 18M6 6l12 12"/>
                            </svg>
                        )}
                        <span class={`text-sm truncate ${addon.status === 'error' ? 'text-red-400' : addon.status === 'done' ? 'text-w-sub' : 'text-w-text'}`}>
                            {addon.status === 'fetching' && tf('discover.fetchingAddon', addon.host)}
                            {addon.status === 'done' && tf('discover.addonStreams', addon.host, addon.count)}
                            {addon.status === 'error' && tf('discover.errorFetchingAddon', addon.host)}
                        </span>
                    </div>
                ))}
            </div>
        </div>
    );
}

// --- Stream Content ---

function is4kStream(parsedInfo) {
    return parsedInfo.labels.some(l => l === '4K');
}

function isSeasonPack(title) {
    const cleanTitle = String(title || '').toLowerCase();
    // 1. Explicit season words/ranges: "season 1", "season 1-3", "seasons 1-5", "complete season", "complete series"
    if (/\b(?:seasons?|temporadas?)\s*\d{1,2}(?:\s*-\s*\d{1,2})?\b/.test(cleanTitle)) return true;
    if (/\bcomplete\s+season\b/.test(cleanTitle)) return true;
    if (/\bcomplete\s+series\b/.test(cleanTitle)) return true;
    if (/\bseason\s*complete\b/.test(cleanTitle)) return true;
    
    // 2. Season range: "s01-s03", "s01-03", "s1-3"
    if (/\bs\d{1,2}\s*-\s*s?\d{1,2}\b/.test(cleanTitle)) return true;

    // 3. Episode ranges: "s01e01-12", "s1e01-e12", "s01e01-s01e12", "s5e1-8"
    if (/\bs\d{1,2}e\d{1,2}\s*-\s*(?:e?\d{1,2})\b/.test(cleanTitle)) return true;
    if (/\bs\d{1,2}e\d{1,2}\s*-\s*\d{1,2}\b/.test(cleanTitle)) return true;
    if (/\bs\d{1,2}e\d{1,2}\s*(?:of|\/)\s*\d{1,2}\b/.test(cleanTitle)) return true;
    if (/\bs\d{1,2}\s*e\d{1,2}\s*-\s*e?\d{1,2}\b/.test(cleanTitle)) return true;
    if (/\bs\d{1,2}\s*e\d{1,2}\s*-\s*\d{1,2}\b/.test(cleanTitle)) return true;
    if (/\b(?:episodes?|eps?)\s*\d{1,2}\s*-\s*\d{1,2}\b/.test(cleanTitle)) return true;

    // 4. "s01" or "s1" alone (without single episode "eXX" marker)
    // E.g. "The Boys S05 2160p" but not "The Boys S05E01 2160p"
    if (/\bs\d{1,2}\b/.test(cleanTitle)) {
        if (!/\bs\d{1,2}\s*e\d{1,2}\b/.test(cleanTitle) && !/\bs\d{1,2}e\d{1,2}\b/.test(cleanTitle)) {
            return true;
        }
    }
    return false;
}

const EXTRA_LABEL_PATTERNS = [
    { label: '8K', re: /\b(?:4320p|8k)\b/i },
    { label: '4K', re: /\b(?:2160p|4k|uhd)\b/i },
    { label: '1080p', re: /\b1080p\b/i },
    { label: '720p', re: /\b720p\b/i },
    { label: '480p', re: /\b(?:480p|sd|576p)\b/i },
    { label: 'Season', re: /\b(?:seasons?|temporadas?)\s*\d{1,2}(?:\s*-\s*\d{1,2})?\b|\bcomplete\s+season\b|\bcomplete\s+series\b|\bseason\s*complete\b|\bs\d{1,2}\s*-\s*s?\d{1,2}\b|\bs\d{1,2}e\d{1,2}\s*-\s*(?:e?\d{1,2})\b|\bs\d{1,2}e\d{1,2}\s*(?:of|\/)\s*\d{1,2}\b|\bs\d{1,2}e\d{1,2}\s*-\s*\d{1,2}\b|\bs\d{1,2}\s*e\d{1,2}\s*-\s*e?\d{1,2}\b|\bs\d{1,2}\s*e\d{1,2}\s*-\s*\d{1,2}\b/i },
    { label: 'Pack', re: /\b(?:pack|collection|siterip|playlist|discography|anthology|trilogy|quadrilogy|tetralogy|duology)\b|\b(?:[4-9]|\d{2,})\s*(?:videos?|files?)\b/i },
    { label: 'DV', re: /\b(?:dolby[ .-]?vision|dovi|dv)\b/i },
    { label: 'HDR10+', re: /\b(?:hdr10\+|hdr10plus)\b/i },
    { label: 'HDR10', re: /\bhdr10\b/i },
    { label: 'HDR', re: /\bhdr\b/i },
    { label: 'REMUX', re: /\bremux\b/i },
    { label: 'BluRay', re: /\bblu[ ._-]?ray\b|\bbluray\b/i },
    { label: 'BRRip', re: /\bbrrip\b/i },
    { label: 'BDRip', re: /\bbdrip\b/i },
    { label: 'WEB-DL', re: /\bweb[ ._-]?dl\b/i },
    { label: 'WEBRip', re: /\bwebrip\b/i },
    { label: 'HDTV', re: /\bhdtv\b/i },
    { label: 'DVDRip', re: /\bdvdrip\b/i },
    { label: 'CAM', re: /\b(?:cam|camrip)\b/i },
    { label: 'TS', re: /\b(?:ts|telesync|tc|telecine)\b/i },
    { label: 'Multi-Audio', re: /\b(?:multi[ ._-]?(?:audio|lang|language)|multiaudio|multi-audio|multi)\b/i },
    { label: 'Dual-Audio', re: /\b(?:dual[ ._-]?(?:audio|lang|language)|dualaudio|dual-audio|dual)\b/i },
    { label: 'Dubbed', re: /\b(?:dubbed|dub)\b/i },
    { label: 'Subbed', re: /\b(?:subbed|sub)\b/i },
    { label: 'Multi-Sub', re: /\b(?:multi[ ._-]?(?:sub|subs|subtitle|subtitles)|multisub|multi-sub)\b/i },
    { label: 'HEVC', re: /\b(?:hevc|h[ ._-]?265|x265)\b/i },
    { label: 'AVC', re: /\b(?:avc|h[ ._-]?264|x264)\b/i },
    { label: 'AV1', re: /\bav1\b/i },
    { label: 'VP9', re: /\bvp9\b/i },
    { label: '10bit', re: /\b10[ ._-]?bit\b/i },
    { label: '8bit', re: /\b8[ ._-]?bit\b/i },
    { label: 'Atmos', re: /\batmos\b/i },
    { label: 'TrueHD', re: /\btruehd\b/i },
    { label: 'DTS-HD', re: /\bdts[ ._-]?hd\b/i },
    { label: 'DTS-X', re: /\bdts[ ._-]?x\b/i },
    { label: 'DTS', re: /\bdts\b/i },
    { label: 'DD+', re: /\b(?:ddp|dd\+|eac3|e-ac-3)\b/i },
    { label: 'AC3', re: /\bac3\b/i },
    { label: 'AAC', re: /\baac\b/i },
    { label: 'FLAC', re: /\bflac\b/i },
    { label: 'OPUS', re: /\bopus\b/i },
    { label: 'MP3', re: /\bmp3\b/i },
    { label: 'Stereo', re: /\b(?:stereo|2\.0|2ch)\b/i },
    { label: '5.1', re: /\b(?:5\.1|6ch)\b/i },
    { label: '7.1', re: /\b(?:7\.1|8ch)\b/i },
    { label: '.mkv', re: /\.mkv\b/i },
    { label: '.mp4', re: /\.mp4\b/i },
    { label: '.avi', re: /\.avi\b/i },
];

const LABEL_ORDER = [
    '8K', '4K', '1080p', '720p', '480p',
    'Season', 'Pack',
    'DV', 'HDR10+', 'HDR10', 'HDR',
    'REMUX', 'BluRay', 'BRRip', 'BDRip', 'WEB-DL', 'WEBRip', 'HDTV', 'DVDRip', 'CAM', 'TS',
    'Multi-Audio', 'Dual-Audio', 'Dubbed', 'Subbed', 'Multi-Sub',
    'HEVC', 'AV1', 'AVC', 'VP9', '10bit', '8bit',
    'Atmos', 'TrueHD', 'DTS-HD', 'DTS-X', 'DTS', 'DD+', 'AC3', 'AAC', 'FLAC', 'OPUS', 'MP3', 'Stereo', '5.1', '7.1',
    '.mkv', '.mp4', '.avi',
];

function canonicalLabel(label) {
    const raw = String(label || '').trim();
    const compact = raw.toLowerCase().replace(/[\s._\-\[\]]+/g, '');
    if (compact === '4320p' || compact === '8k') return '8K';
    if (compact === '2160p' || compact === '4k' || compact === 'uhd') return '4K';
    if (compact === '1080p') return '1080p';
    if (compact === '720p') return '720p';
    if (compact === '480p' || compact === 'sd' || compact === '576p') return '480p';
    if (compact === 'season') return 'Season';
    if (compact === 'pack') return 'Pack';
    if (compact === 'dolbyvision' || compact === 'dovi' || compact === 'dv') return 'DV';
    if (compact === 'hdr10+' || compact === 'hdr10plus') return 'HDR10+';
    if (compact === 'hdr10') return 'HDR10';
    if (compact === 'hdr') return 'HDR';
    if (compact === 'webdl') return 'WEB-DL';
    if (compact === 'webrip') return 'WEBRip';
    if (compact === 'bluray' || compact === 'blurayrip') return 'BluRay';
    if (compact === 'brrip') return 'BRRip';
    if (compact === 'bdrip') return 'BDRip';
    if (compact === 'hdtv') return 'HDTV';
    if (compact === 'dvdrip') return 'DVDRip';
    if (compact === 'cam' || compact === 'camrip') return 'CAM';
    if (compact === 'ts' || compact === 'telesync' || compact === 'tc' || compact === 'telecine') return 'TS';
    if (compact === 'multiaudio' || compact === 'multi' || compact === 'multi-audio') return 'Multi-Audio';
    if (compact === 'dualaudio' || compact === 'dual' || compact === 'dual-audio') return 'Dual-Audio';
    if (compact === 'dubbed' || compact === 'dub') return 'Dubbed';
    if (compact === 'subbed' || compact === 'sub') return 'Subbed';
    if (compact === 'multisub') return 'Multi-Sub';
    if (compact === 'h265' || compact === 'x265' || compact === 'hevc') return 'HEVC';
    if (compact === 'av1') return 'AV1';
    if (compact === 'h264' || compact === 'x264' || compact === 'avc') return 'AVC';
    if (compact === 'vp9') return 'VP9';
    if (compact === '10bit') return '10bit';
    if (compact === '8bit') return '8bit';
    if (compact === 'atmos') return 'Atmos';
    if (compact === 'truehd') return 'TrueHD';
    if (compact === 'dtshd') return 'DTS-HD';
    if (compact === 'dtsx') return 'DTS-X';
    if (compact === 'dts') return 'DTS';
    if (compact === 'ddp' || compact === 'dd+' || compact === 'eac3' || compact === 'eac') return 'DD+';
    if (compact === 'ac3') return 'AC3';
    if (compact === 'aac') return 'AAC';
    if (compact === 'flac') return 'FLAC';
    if (compact === 'opus') return 'OPUS';
    if (compact === 'mp3') return 'MP3';
    if (compact === 'stereo' || compact === '2.0' || compact === '2ch') return 'Stereo';
    if (compact === '5.1' || compact === '6ch') return '5.1';
    if (compact === '7.1' || compact === '8ch') return '7.1';
    if (compact === 'mkv') return '.mkv';
    if (compact === 'mp4') return '.mp4';
    if (compact === 'avi') return '.avi';
    return raw;
}

function sortLabels(labels) {
    return [...labels].sort((a, b) => {
        const ai = LABEL_ORDER.indexOf(a);
        const bi = LABEL_ORDER.indexOf(b);
        if (ai !== -1 && bi !== -1) return ai - bi;
        if (ai !== -1) return -1;
        if (bi !== -1) return 1;
        return a.localeCompare(b);
    });
}

function parseStreamSize(stream) {
    const text = `${stream.title || ''}\n${stream.name || ''}\n${stream.description || ''}`;
    const match = text.match(/\b(\d+(?:\.\d+)?)\s*(GB|MB|KB|GiB|MiB|KiB)\b/i);
    if (!match) return 0;
    const num = parseFloat(match[1]);
    const unit = match[2].toLowerCase();
    if (unit.startsWith('g')) return num * 1024 * 1024 * 1024;
    if (unit.startsWith('m')) return num * 1024 * 1024;
    if (unit.startsWith('k')) return num * 1024;
    return 0;
}

function parseStreamSeeds(stream) {
    const text = `${stream.title || ''}\n${stream.name || ''}\n${stream.description || ''}`;
    const emojiMatch = text.match(/[👤👥]\s*(\d+)/u);
    if (emojiMatch) return parseInt(emojiMatch[1], 10);

    const labelMatch = text.match(/\b(?:seeds|seeders|seed)\s*:\s*(\d+)\b/i);
    if (labelMatch) return parseInt(labelMatch[1], 10);

    const suffixMatch = text.match(/\b(\d+)\s*(?:seeds|seeders|seed)\b/i);
    if (suffixMatch) return parseInt(suffixMatch[1], 10);

    const sMatch = text.match(/\bs\s*:\s*(\d+)\b/i);
    if (sMatch) {
        if (/([pl]\s*:\s*\d+|peers|leechers)/i.test(text)) {
            return parseInt(sMatch[1], 10);
        }
    }
    return 0;
}

function enrichStreamInfo(stream) {
    const info = parseStreamName(stream.name);
    const text = `${stream.name || ''}\n${stream.title || ''}`;
    const labels = [];
    const seen = new Set();
    for (const raw of info.labels) {
        const label = canonicalLabel(raw);
        const key = label.toLowerCase();
        if (!seen.has(key)) {
            seen.add(key);
            labels.push(label);
        }
    }
    for (const { label, re } of EXTRA_LABEL_PATTERNS) {
        const key = label.toLowerCase();
        if (!seen.has(key) && re.test(text)) {
            seen.add(key);
            labels.push(label);
        }
    }

    const cleanTitle = text.toLowerCase();
    const isSeason = isSeasonPack(text);
    let isPack = false;

    // Pack matches keywords or files >= 5
    if (/\b(?:pack|collection|siterip|playlist|discography|anthology|trilogy|quadrilogy|tetralogy|duology)\b/i.test(cleanTitle)) {
        isPack = true;
    }
    if (/\b(?:[4-9]|\d{2,})\s*(?:videos?|files?)\b/i.test(cleanTitle)) {
        isPack = true;
    }
    if (stream.files >= 5) {
        isPack = true;
    }
    if (isSeason) {
        isPack = true; // pack is backup of season
    }

    if (isSeason && !seen.has('season')) {
        seen.add('season');
        labels.push('Season');
    }
    if (isPack && !seen.has('pack')) {
        seen.add('pack');
        labels.push('Pack');
    }

    info.labels = sortLabels(labels);
    info.size = parseStreamSize(stream);
    info.seeds = parseStreamSeeds(stream);
    return info;
}

function streamResolutionKey(parsedInfo) {
    const labels = parsedInfo.labels.map(l => l.toLowerCase());
    if (labels.includes('8k') || labels.includes('4320p')) return '8k';
    if (is4kStream(parsedInfo) || labels.includes('2160p') || labels.includes('4k')) return '4k';
    if (labels.includes('1080p')) return '1080p';
    if (labels.includes('720p')) return '720p';
    return 'other';
}

function getEnabledResolutionSet(settings) {
    const raw = settings?.preferred_resolutions || settings?.preferredResolutions || [];
    if (!Array.isArray(raw) || raw.length === 0) return new Set(['1080p', '720p', 'other']);
    const enabled = raw
        .filter(r => r && r.enabled !== false && r.Enabled !== false)
        .map(r => String(r.resolution || r.Resolution || '').toLowerCase())
        .filter(Boolean);
    return new Set(enabled);
}

function getPreferredLanguage(settings) {
    const code = String(settings?.preferred_language || settings?.preferredLanguage || '').trim().toLowerCase();
    if (!code) return null;
    return LANG_MAP[code] || null;
}

function streamLanguageNames(stream) {
    const text = `${stream.title || ''}\n${stream.name || ''}`;
    return extractLanguages(text).map(l => l.name);
}

function streamSearchText(stream, parsedInfo, langs) {
    return [
        parsedInfo.source,
        ...parsedInfo.labels,
        ...langs,
        stream.name,
        stream.title,
        stream.description,
        stream.infoHash,
        stream.fileIdx,
        stream.url,
        stream.externalUrl,
        stream.behaviorHints && JSON.stringify(stream.behaviorHints),
    ].filter(v => v != null && v !== '').join(' ').toLowerCase();
}

const FILTER_GROUPS = {
    resolution: ['8K', '4K', '1080p', '720p', '480p'],
    seasonPack: ['Season', 'Pack'],
    videoRange: ['DV', 'HDR10+', 'HDR10', 'HDR'],
    sourceRelease: ['REMUX', 'BluRay', 'BRRip', 'BDRip', 'WEB-DL', 'WEBRip', 'HDTV', 'DVDRip', 'CAM', 'TS'],
    audioSubtitle: ['Multi-Audio', 'Dual-Audio', 'Dubbed', 'Subbed', 'Multi-Sub'],
    videoCodecs: ['HEVC', 'AV1', 'AVC', 'VP9', '10bit', '8bit'],
    audioCodecs: ['Atmos', 'TrueHD', 'DTS-HD', 'DTS-X', 'DTS', 'DD+', 'AC3', 'AAC', 'FLAC', 'OPUS', 'MP3', 'Stereo', '5.1', '7.1'],
    containers: ['.mkv', '.mp4', '.avi'],
};

const HIGH_VALUE_LABELS = ['8K', '4K', '1080p', '720p', '480p', 'Season', 'Pack'];

function getLabelGroup(label) {
    const lower = String(label || '').toLowerCase();
    for (const [group, labels] of Object.entries(FILTER_GROUPS)) {
        if (labels.some(l => l.toLowerCase() === lower)) {
            return group;
        }
    }
    return null;
}

function StreamContent({ modal, onStreamClick, hasCustomAddons, onSetupAddons, onRetryStreams, onLoadMore, statusButtons, headerMeta, stremioSettings = {} }) {
    const { title, poster, streams, error, failedAddons } = modal;
    const failed = failedAddons || [];
    const [retrying, setRetrying] = useState(false);
    const [streamQuery, setStreamQuery] = useState('');
    const [showAdvanced, setShowAdvanced] = useState(false);
    const [sortBy, setSortBy] = useState('default');
    const isAdult = modal.itemType === 'porn' || modal.itemType === 'jav' || modal.itemType === 'adult';

    const handleRetry = useCallback(async (e) => {
        e?.stopPropagation?.();
        if (retrying || !onRetryStreams) return;
        setRetrying(true);
        try { await onRetryStreams(); } finally { setRetrying(false); }
    }, [onRetryStreams, retrying]);

    const parsed = useMemo(() => streams.map(s => enrichStreamInfo(s)), [streams]);

    const streamLangs = useMemo(() =>
        streams.map(s => streamLanguageNames(s)),
        [streams]
    );

    const { baseStreams, baseParsed, baseLangs } = useMemo(() => {
        return {
            baseStreams: streams,
            baseParsed: parsed,
            baseLangs: streamLangs,
        };
    }, [streams, parsed, streamLangs]);

    const { allSources, allLabels, allLangs } = useMemo(() => {
        const sources = [];
        const labels = [];
        const seenLabelsLower = {};
        for (const info of baseParsed) {
            if (!sources.includes(info.source)) sources.push(info.source);
            for (const lbl of info.labels) {
                const lower = lbl.toLowerCase();
                if (!seenLabelsLower[lower]) {
                    seenLabelsLower[lower] = true;
                    labels.push(lbl);
                }
            }
        }
        const preferredLang = getPreferredLanguage(stremioSettings);
        const langs = [];
        const seenLangs = new Set();
        if (preferredLang && baseLangs.some(langsForStream => langsForStream.includes(preferredLang.name))) {
            langs.push(preferredLang);
            seenLangs.add(preferredLang.name);
        }
        const hindiLang = LANG_MAP['hi'];
        if (hindiLang && !seenLangs.has(hindiLang.name) && baseLangs.some(langsForStream => langsForStream.includes(hindiLang.name))) {
            langs.push(hindiLang);
            seenLangs.add(hindiLang.name);
        }
        return { allSources: sources, allLabels: sortLabels(labels), allLangs: langs };
    }, [baseParsed, baseLangs, stremioSettings]);

    const [activeSources, setActiveSources] = useState(() => {
        const prefs = loadPrefs();
        if (!prefs.sources) return {};
        const result = {};
        for (const src of prefs.sources) {
            if (allSources.includes(src)) result[src] = true;
        }
        return result;
    });

    const [activeLabels, setActiveLabels] = useState(() => {
        const prefs = loadPrefs();
        if (!prefs.labels) return {};
        const result = {};
        for (const lbl of prefs.labels) {
            if (getLabelGroup(lbl) && allLabels.some(l => l.toLowerCase() === lbl.toLowerCase())) {
                result[lbl] = true;
            }
        }
        return result;
    });

    const [activeLang, setActiveLang] = useState(null);

    const filteredStreams = useMemo(() => {
        const searchTerms = streamQuery.trim().toLowerCase().split(/\s+/).filter(Boolean);
        const activeSrcKeys = Object.keys(activeSources).filter(k => activeSources[k]);
        const activeLblKeys = Object.keys(activeLabels).filter(k => activeLabels[k]);

        const activeGroups = {};
        if (activeSrcKeys.length > 0) {
            activeGroups['sources'] = activeSrcKeys;
        }
        if (activeLang) {
            activeGroups['languages'] = [activeLang];
        }
        for (const lbl of activeLblKeys) {
            const group = getLabelGroup(lbl);
            if (group) {
                if (!activeGroups[group]) {
                    activeGroups[group] = [];
                }
                activeGroups[group].push(lbl.toLowerCase());
            }
        }

        const enabledResolutions = isAdult ? new Set(['8k', '4k', '1080p', '720p', 'other']) : getEnabledResolutionSet(stremioSettings);
        const hasActiveResolution = !!activeGroups['resolution'];

        return baseStreams.map((s, i) => {
            let show = true;

            for (const [groupName, activeFilters] of Object.entries(activeGroups)) {
                if (groupName === 'sources') {
                    if (!activeFilters.includes(baseParsed[i].source)) {
                        show = false;
                        break;
                    }
                } else if (groupName === 'languages') {
                    if (!baseLangs[i].includes(activeLang)) {
                        show = false;
                        break;
                    }
                } else {
                    const streamLabelsLower = baseParsed[i].labels.map(l => l.toLowerCase());
                    const matchesAny = activeFilters.some(filterLower => streamLabelsLower.includes(filterLower));
                    if (!matchesAny) {
                        show = false;
                        break;
                    }
                }
            }

            if (show && !hasActiveResolution) {
                const resKey = streamResolutionKey(baseParsed[i]);
                if (!enabledResolutions.has(resKey)) {
                    show = false;
                }
            }

            if (show && searchTerms.length > 0) {
                const haystack = streamSearchText(s, baseParsed[i], baseLangs[i]);
                if (!searchTerms.every(term => haystack.includes(term))) {
                    show = false;
                }
            }

            return { stream: s, parsed: baseParsed[i], langs: baseLangs[i], visible: show };
        });
    }, [baseStreams, baseParsed, baseLangs, activeSources, activeLabels, activeLang, streamQuery, stremioSettings, modal.itemType]);

    const sortedFilteredStreams = useMemo(() => {
        const items = filteredStreams.map((item, index) => ({ ...item, index }));
        if (sortBy === 'seeds') {
            items.sort((a, b) => {
                const diff = (b.parsed.seeds || 0) - (a.parsed.seeds || 0);
                if (diff !== 0) return diff;
                const sizeDiff = (b.parsed.size || 0) - (a.parsed.size || 0);
                if (sizeDiff !== 0) return sizeDiff;
                return a.index - b.index;
            });
        } else if (sortBy === 'size') {
            items.sort((a, b) => {
                const diff = (b.parsed.size || 0) - (a.parsed.size || 0);
                if (diff !== 0) return diff;
                const seedsDiff = (b.parsed.seeds || 0) - (a.parsed.seeds || 0);
                if (seedsDiff !== 0) return seedsDiff;
                return a.index - b.index;
            });
        }
        return items;
    }, [filteredStreams, sortBy]);

    const toggleSort = useCallback(() => {
        setSortBy(prev => {
            if (prev === 'default') return 'seeds';
            if (prev === 'seeds') return 'size';
            return 'default';
        });
    }, []);

    const getSortLabel = useCallback((mode) => {
        if (mode === 'seeds') {
            const lbl = t('discover.sortSeeds');
            return lbl === 'discover.sortSeeds' ? 'Seeds' : lbl;
        }
        if (mode === 'size') {
            const lbl = t('discover.sortSize');
            return lbl === 'discover.sortSize' ? 'Size' : lbl;
        }
        const lbl = t('discover.sortDefault');
        return lbl === 'discover.sortDefault' ? 'Default' : lbl;
    }, []);

    const visibleCount = filteredStreams.filter(s => s.visible).length;
    const hasSearchQuery = streamQuery.trim().length > 0;
    const hasActiveFilters = Object.keys(activeSources).length > 0 || Object.keys(activeLabels).length > 0 || activeLang || hasSearchQuery;

    const subtitleText = useMemo(() => {
        const total = baseStreams.length;
        if (hasActiveFilters) {
            return tf('discover.streamsFiltered', visibleCount, total);
        }
        return tf('discover.streamsFound', total);
    }, [hasActiveFilters, visibleCount, baseStreams.length]);

    const toggleSource = useCallback((src) => {
        setActiveSources(prev => {
            const next = { ...prev };
            if (next[src]) delete next[src];
            else next[src] = true;
            savePrefs({ sources: Object.keys(next) });
            return next;
        });
    }, []);

    const toggleLabel = useCallback((lbl) => {
        setActiveLabels(prev => {
            const next = { ...prev };
            if (next[lbl]) delete next[lbl];
            else next[lbl] = true;
            savePrefs({ labels: Object.keys(next) });
            return next;
        });
    }, []);

    const toggleLang = useCallback((langName) => {
        setActiveLang(prev => {
            const next = prev === langName ? null : langName;
            return next;
        });
    }, []);

    const highValueLabelsToShow = useMemo(() => {
        return HIGH_VALUE_LABELS.filter(lbl =>
            allLabels.some(l => l.toLowerCase() === lbl.toLowerCase())
        );
    }, [allLabels]);

    const hasAdvancedFilters = useMemo(() => {
        if (allSources.length > 1) return true;
        if (allLangs.length > 0) return true;
        return allLabels.some(lbl => {
            const group = getLabelGroup(lbl);
            return group && group !== 'resolution' && group !== 'seasonPack';
        });
    }, [allSources, allLabels, allLangs]);

    if (streams.length === 0) {
        if (failed.length > 0) {
            const onlyFailure = failed[0];
            const headline = failed.length === 1
                ? tf('discover.streamsFailedOne', onlyFailure.name || onlyFailure.host)
                : tf('discover.streamsFailedMany', failed.length);
            return (
                <div>
                    <ModalHeader title={title} poster={poster} subtitle={t('discover.streamsFailedTitle')} extra={statusButtons} {...headerMeta} />
                    <div class="text-center py-4">
                        <svg class="w-12 h-12 text-yellow-400/50 mx-auto mb-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4">
                            <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z"/>
                        </svg>
                        <p class="text-sm text-w-text mb-1">{headline}</p>
                        <p class="text-xs text-w-muted mb-4">{t('discover.streamsFailedBody')}</p>
                        {failed.length > 1 && (
                            <ul class="text-xs text-w-sub mb-4 inline-block text-left">
                                {failed.map(f => (
                                    <li key={f.host}>· {f.name || f.host}</li>
                                ))}
                            </ul>
                        )}
                        <div class="flex justify-center gap-2">
                            <button
                                class="btn btn-soft-cyan btn-sm"
                                onClick={handleRetry}
                                disabled={retrying}
                            >
                                {retrying && <span class="loading loading-spinner loading-xs"></span>}
                                {t('discover.retry')}
                            </button>
                        </div>
                    </div>
                </div>
            );
        }
        return (
            <div>
                <ModalHeader title={title} poster={poster} subtitle={subtitleText} extra={statusButtons} {...headerMeta} />
                <div class="text-center py-6">
                    <p class="text-w-muted text-sm">
                        {error || t('discover.noStreams')}
                    </p>
                    {!hasCustomAddons && (
                        <>
                            <p class="text-w-sub text-xs mt-2 mb-4">
                                {t('discover.installAddonsHint')}
                            </p>
                            <button
                                class="btn btn-ghost btn-sm border border-w-line hover:border-w-cyan/30 hover:text-w-cyan"
                                onClick={onSetupAddons}
                            >
                                {t('discover.setupAddonsBtn')}
                            </button>
                        </>
                    )}
                </div>
            </div>
        );
    }

    return (
        <div>
            <ModalHeader title={title} poster={poster} subtitle={subtitleText} {...headerMeta}
                extra={statusButtons}
            />

            {failed.length > 0 && (
                <div class="mb-3 flex items-center gap-2 px-3 py-2 rounded-lg border border-yellow-500/30 bg-yellow-500/5">
                    <svg class="w-4 h-4 text-yellow-400 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z"/>
                    </svg>
                    <span class="text-xs text-yellow-100/85 flex-1 min-w-0">
                        {failed.length === 1
                            ? tf('discover.streamsPartialFailureOne', failed[0].name || failed[0].host)
                            : tf('discover.streamsPartialFailureMany', failed.length)}
                    </span>
                    <button
                        class="btn btn-ghost btn-xs text-yellow-100/85 hover:bg-yellow-500/10 hover:text-yellow-100 flex-shrink-0"
                        onClick={handleRetry}
                        disabled={retrying}
                    >
                        {retrying && <span class="loading loading-spinner loading-xs"></span>}
                        {t('discover.retry')}
                    </button>
                </div>
            )}

            <div class="mb-3 flex flex-col sm:flex-row sm:items-center gap-3 w-full">
                <div class="w-full sm:w-1/2 flex items-center gap-2">
                    <div class="flex-1 min-w-0">
                        <StreamSearch value={streamQuery} onChange={setStreamQuery} />
                    </div>
                    <button
                        type="button"
                        onClick={toggleSort}
                        class="btn btn-sm border border-w-line bg-w-surface text-w-text hover:border-w-cyan/30 hover:text-w-cyan h-9 gap-1.5 px-3 flex-shrink-0"
                        title={getSortLabel(sortBy)}
                        aria-label={getSortLabel(sortBy)}
                    >
                        {sortBy === 'default' && (
                            <>
                                <svg class="w-4 h-4 text-w-muted" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                                    <path stroke-linecap="round" stroke-linejoin="round" d="M3 4h13M3 8h9m-9 4h6m4 0l4-4m0 0l4 4m-4-4v12" />
                                </svg>
                                <span class="text-xs">{getSortLabel(sortBy)}</span>
                            </>
                        )}
                        {sortBy === 'seeds' && (
                            <>
                                <span class="text-w-cyan text-sm">👤</span>
                                <span class="text-w-cyan text-xs font-semibold">{getSortLabel(sortBy)}</span>
                            </>
                        )}
                        {sortBy === 'size' && (
                            <>
                                <span class="text-w-cyan text-sm">💾</span>
                                <span class="text-w-cyan text-xs font-semibold">{getSortLabel(sortBy)}</span>
                            </>
                        )}
                    </button>
                </div>
                <div class="w-full sm:w-1/2 flex items-center justify-between sm:justify-end gap-2 flex-wrap">
                    <div class="flex items-center gap-1.5 flex-wrap">
                        {highValueLabelsToShow.map(lbl => {
                            const actualLabel = allLabels.find(l => l.toLowerCase() === lbl.toLowerCase()) || lbl;
                            const isActive = !!activeLabels[actualLabel];
                            return (
                                <button
                                    key={`high-val-${actualLabel}`}
                                    class={chipClass(isActive, 'xs')}
                                    onClick={() => toggleLabel(actualLabel)}
                                >
                                    {actualLabel}
                                </button>
                            );
                        })}
                    </div>
                    {hasAdvancedFilters && (
                        <button
                            type="button"
                            class={`btn btn-xs gap-1.5 ${
                                showAdvanced
                                    ? 'bg-o-primary/15 border border-o-primary/30 text-o-primary'
                                    : 'btn-ghost border border-o-line text-o-sub hover:border-o-primary/30 hover:text-o-primary'
                            }`}
                            onClick={() => setShowAdvanced(!showAdvanced)}
                        >
                            <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3"></polygon>
                            </svg>
                            {t('discover.filters')}
                            {showAdvanced ? (
                                <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                    <polyline points="18 15 12 9 6 15"></polyline>
                                </svg>
                            ) : (
                                <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                    <polyline points="6 9 12 15 18 9"></polyline>
                                </svg>
                            )}
                        </button>
                    )}
                </div>
            </div>

            {showAdvanced && hasAdvancedFilters && (
                <div class="mb-4 p-3 sm:p-4 rounded-xl border border-w-line/50 bg-w-surface/30 flex flex-col gap-3.5 transition-all">
                    {allSources.length > 1 && (
                        <div>
                            <div class="text-[11px] font-semibold text-w-muted uppercase tracking-wider mb-1.5">{t('discover.sources')}</div>
                            <div class="flex flex-wrap gap-1.5">
                                {allSources.map(src => (
                                    <button
                                        key={`src-${src}`}
                                        class={chipClass(!!activeSources[src], 'xs')}
                                        onClick={() => toggleSource(src)}
                                    >
                                        {src}
                                    </button>
                                ))}
                            </div>
                        </div>
                    )}

                    {allLangs.length > 0 && (
                        <div>
                            <div class="text-[11px] font-semibold text-w-muted uppercase tracking-wider mb-1.5">{t('discover.languages')}</div>
                            <div class="flex flex-wrap gap-1.5">
                                {allLangs.map(lang => (
                                    <button
                                        key={`lang-${lang.name}`}
                                        class={chipClass(activeLang === lang.name, 'xs')}
                                        onClick={() => toggleLang(lang.name)}
                                    >
                                        {lang.flag} {lang.name}
                                    </button>
                                ))}
                            </div>
                        </div>
                    )}

                    {Object.entries(FILTER_GROUPS).map(([groupKey, groupLabels]) => {
                        if (groupKey === 'resolution' || groupKey === 'pack') return null;

                        const availableLabels = groupLabels.filter(lbl =>
                            allLabels.some(l => l.toLowerCase() === lbl.toLowerCase())
                        );

                        if (availableLabels.length === 0) return null;

                        const groupTitleKey = `discover.${groupKey}`;
                        const groupTitle = t(groupTitleKey) || groupKey;

                        return (
                            <div key={groupKey}>
                                <div class="text-[11px] font-semibold text-w-muted uppercase tracking-wider mb-1.5">{groupTitle}</div>
                                <div class="flex flex-wrap gap-1.5">
                                    {availableLabels.map(lbl => {
                                        const actualLabel = allLabels.find(l => l.toLowerCase() === lbl.toLowerCase()) || lbl;
                                        return (
                                            <button
                                                key={`lbl-${actualLabel}`}
                                                class={chipClass(!!activeLabels[actualLabel], 'xs')}
                                                onClick={() => toggleLabel(actualLabel)}
                                            >
                                                {actualLabel}
                                            </button>
                                        );
                                    })}
                                </div>
                            </div>
                        );
                    })}
                </div>
            )}

            {baseStreams.length === 0 ? (
                <div>
                    <p class="text-w-muted text-sm text-center py-6">
                        {t('discover.noProfileStreamMatch')}
                    </p>
                    {isAdult && !modal.exhaustive && onLoadMore && (
                        <button
                            type="button"
                            class="btn btn-sm w-full border border-w-line bg-w-surface/50 text-w-text hover:border-w-cyan/30 hover:text-w-cyan py-2.5 transition-all mt-2"
                            onClick={onLoadMore}
                        >
                            ⚡ {t('discover.loadMore') || 'Load More'}
                        </button>
                    )}
                </div>
            ) : (
                <>
                    <div class="flex flex-col gap-2 max-h-[400px] overflow-y-auto">
                        {sortedFilteredStreams.map(({ stream, parsed: info, visible }, i) => (
                            visible && <StreamRow key={i} stream={stream} info={info} onStreamClick={onStreamClick} isAdult={isAdult} />
                        ))}
                    </div>

                    {hasActiveFilters && visibleCount === 0 && (
                        <p class="text-w-muted text-sm text-center py-6">
                            {hasSearchQuery ? t('discover.noStreamSearchMatch') : t('discover.noFilterMatch')}
                        </p>
                    )}

                    {isAdult && !modal.exhaustive && onLoadMore && (
                        <button
                            type="button"
                            class="btn btn-sm w-full border border-w-line bg-w-surface/50 text-w-text hover:border-w-cyan/30 hover:text-w-cyan py-2.5 transition-all mt-4"
                            onClick={onLoadMore}
                        >
                            ⚡ {t('discover.loadMore') || 'Load More'}
                        </button>
                    )}
                </>
            )}
        </div>
    );
}

function StreamSearch({ value, onChange }) {
    return (
        <div class="w-full">
            <div class="relative w-full">
                <svg class="w-4 h-4 absolute left-3 top-1/2 -translate-y-1/2 text-w-muted pointer-events-none z-10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <circle cx="11" cy="11" r="8"></circle>
                    <path d="m21 21-4.3-4.3"></path>
                </svg>
                <input
                    type="text"
                    value={value}
                    onInput={(e) => onChange(e.currentTarget.value)}
                    placeholder={t('discover.searchStreams')}
                    autocomplete="off"
                    class="input input-sm relative w-full h-9 bg-w-surface border-w-line focus:border-w-cyan/50 focus:outline-none text-w-text placeholder:text-w-muted pl-9 pr-8"
                />
                {value && (
                    <button
                        type="button"
                        class="absolute right-2 top-1/2 -translate-y-1/2 p-1 text-w-muted hover:text-w-text transition-colors"
                        onClick={() => onChange('')}
                        aria-label={t('discover.clearSearch')}
                    >
                        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <path d="M18 6 6 18M6 6l12 12"></path>
                        </svg>
                    </button>
                )}
            </div>
        </div>
    );
}

const PLAY_ICON = (
    <svg class="w-4 h-4 text-w-cyan" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <polygon points="5 3 19 12 5 21 5 3"></polygon>
    </svg>
);

function StreamRow({ stream, info, onStreamClick, isAdult }) {
    const infoHash = extractInfoHash(stream);
    const fileIdx = extractFileIdx(stream);
    const magnetUrl = stream.magnetUrl || stream.magnetURL;
    const resource = magnetUrl || infoHash;
    const titleLines = (stream.title || '').split('\n').filter(Boolean);
    const displayedLabels = info.labels.filter(label => !(label === 'Pack' && info.labels.includes('Season')));

    const content = (
        <>
            <div class="flex-shrink-0 w-8 h-8 rounded-full bg-w-cyan/10 flex items-center justify-center">
                {PLAY_ICON}
            </div>
            <div class="min-w-0 flex-1">
                <div class="flex items-center gap-1.5 flex-wrap">
                    <span class="text-sm font-medium">{info.source}</span>
                    {displayedLabels.map(label => (
                        <span key={label} class="bg-w-cyan/10 text-w-cyan text-[10px] px-1.5 py-0.5 rounded font-medium">{label}</span>
                    ))}
                </div>
                {titleLines.map((line, i) => (
                    <div key={i} class={`text-xs text-w-sub ${isAdult && i === 0 ? 'break-all' : 'line-clamp-1'}`}>{line}</div>
                ))}
            </div>
            {!infoHash && (
                <span class="text-xs text-w-muted flex-shrink-0">{t('discover.noTorrent')}</span>
            )}
        </>
    );

    if (infoHash) {
        return (
            <div
                onClick={() => onStreamClick(resource, fileIdx)}
                class="cursor-pointer flex items-center gap-3 p-3 rounded-lg border border-w-line hover:border-w-cyan/30 hover:bg-w-surface/50 transition-all"
            >
                {content}
            </div>
        );
    }

    return (
        <div class="opacity-50 flex items-center gap-3 p-3 rounded-lg border border-w-line hover:border-w-cyan/30 hover:bg-w-surface/50 transition-all">
            {content}
        </div>
    );
}

// --- Episode Picker ---

function EpisodePicker({ modal, onEpisodeSelect, defaultSeason, onSeasonChange, statusButtons, headerMeta }) {
    const { title, poster, meta } = modal;
    const videos = meta?.videos || [];

    const { seasons, seasonNums } = useMemo(() => {
        const s = {};
        for (const v of videos) {
            const sn = v.season != null ? v.season : 0;
            if (!s[sn]) s[sn] = [];
            s[sn].push(v);
        }
        const nums = Object.keys(s).map(Number).sort((a, b) => {
            if (a === 0) return 1;
            if (b === 0) return -1;
            return a - b;
        });
        return { seasons: s, seasonNums: nums };
    }, [videos]);

    const [activeSeason, setActiveSeason] = useState(() => {
        if (defaultSeason != null && seasonNums.includes(Number(defaultSeason))) {
            return Number(defaultSeason);
        }
        return seasonNums[0] ?? 0;
    });

    // Sync activeSeason when defaultSeason changes (e.g. popstate — component already mounted, useState initializer won't re-run)
    useEffect(() => {
        if (defaultSeason != null && seasonNums.includes(Number(defaultSeason))) {
            setActiveSeason(Number(defaultSeason));
        } else if (defaultSeason == null) {
            setActiveSeason(seasonNums[0] ?? 0);
        }
    }, [defaultSeason]);

    const episodes = useMemo(() =>
        (seasons[activeSeason] || []).slice().sort((a, b) => (a.episode || 0) - (b.episode || 0)),
        [seasons, activeSeason]
    );

    if (!videos.length) {
        return (
            <div>
                <ModalHeader title={title} poster={poster} subtitle={t('discover.selectEpisode')} extra={statusButtons} {...headerMeta} />
                <p class="text-w-muted text-sm text-center py-6">{t('discover.noEpisodes')}</p>
            </div>
        );
    }

    return (
        <div>
            <ModalHeader title={title} poster={poster} subtitle={t('discover.selectEpisode')} extra={statusButtons} {...headerMeta} />

            {seasonNums.length > 1 && (
                <div class="flex gap-1.5 mb-3 flex-wrap">
                    {seasonNums.map(sn => (
                        <button
                            key={sn}
                            class={chipClass(sn === activeSeason, 'xs')}
                            onClick={() => { setActiveSeason(sn); if (onSeasonChange) onSeasonChange(sn); }}
                        >
                            {sn === 0 ? t('discover.specials') : `S${sn}`}
                        </button>
                    ))}
                </div>
            )}

            <div class="max-h-[350px] overflow-y-auto">
                <div class="flex flex-col gap-1.5">
                    {episodes.map(episode => (
                        <button
                            key={episode.id || `${episode.season}-${episode.episode}`}
                            class="flex items-center gap-3 p-2.5 rounded-lg border border-w-line hover:border-w-cyan/30 hover:bg-w-surface/50 transition-all w-full text-left cursor-pointer bg-transparent"
                            onClick={() => onEpisodeSelect(episode, modal)}
                        >
                            <span class="flex-shrink-0 w-8 h-8 rounded-full bg-w-cyan/10 flex items-center justify-center text-xs font-bold text-w-cyan">
                                {episode.episode != null ? String(episode.episode) : '?'}
                            </span>
                            <div class="min-w-0 flex-1">
                                <div class="text-sm font-medium line-clamp-1">
                                    {episode.title || episode.name || tf('discover.episodeLabel', episode.episode || '?')}
                                </div>
                                {(episode.released || episode.overview) && (
                                    <div class="text-xs text-w-muted line-clamp-1">
                                        {episode.released
                                            ? new Date(episode.released).toLocaleDateString()
                                            : (episode.overview || '')}
                                    </div>
                                )}
                            </div>
                            <span class="text-w-muted flex-shrink-0">
                                <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                    <path d="M9 18l6-6-6-6"/>
                                </svg>
                            </span>
                        </button>
                    ))}
                </div>
            </div>
        </div>
    );
}
