/**
 * Chromecast plugin — custom integration for Preact Player wrapper.
 * Ports ChromecastPlayer from mediaelement-plugins/chromecast/player.js
 * while stripping out legacy dependencies on mejs and originalNode.
 */

let isSessionListenerAdded = false;
let activeCastPlayer = null;
let currentVideoEl = null;

class ChromecastPlayer {
    constructor(player, controller, videoEl) {
        this.player = player;
        this.controller = controller;
        this.videoEl = videoEl;
        this.endedMedia = false;

        // Save original methods for restoration
        this.originalPlay = videoEl.play;
        this.originalPause = videoEl.pause;
        this.originalLoad = videoEl.load;

        // Pause local video and mute it while casting
        this.videoEl.pause();
        this.videoEl.muted = true;

        // Override methods on videoEl
        this.videoEl.play = () => this.play();
        this.videoEl.pause = () => this.pause();
        this.videoEl.load = () => this.load();

        // Override properties on videoEl
        Object.defineProperties(this.videoEl, {
            paused: { get: () => this.paused, configurable: true },
            currentTime: {
                get: () => this.currentTime,
                set: (val) => { this.currentTime = val; },
                configurable: true
            },
            volume: {
                get: () => this.volume,
                set: (val) => { this.volume = val; },
                configurable: true
            },
            muted: {
                get: () => this.muted,
                set: (val) => { this.muted = val; },
                configurable: true
            },
            duration: { get: () => this.duration, configurable: true }
        });

        // Add event listeners for RemotePlayer changes
        this.listeners = [];

        const addRemoteListener = (type, handler) => {
            this.controller.addEventListener(type, handler);
            this.listeners.push({ type, handler });
        };

        addRemoteListener(cast.framework.RemotePlayerEventType.IS_PAUSED_CHANGED, () => {
            const eventName = this.paused ? 'pause' : 'play';
            this.videoEl.dispatchEvent(new Event(eventName));
            this.endedMedia = false;
        });

        addRemoteListener(cast.framework.RemotePlayerEventType.IS_MUTED_CHANGED, () => {
            this.videoEl.dispatchEvent(new Event('volumechange'));
        });

        addRemoteListener(cast.framework.RemotePlayerEventType.IS_MEDIA_LOADED_CHANGED, () => {
            this.videoEl.dispatchEvent(new Event('loadedmetadata'));
            this.videoEl.dispatchEvent(new Event('canplay'));
        });

        addRemoteListener(cast.framework.RemotePlayerEventType.VOLUME_LEVEL_CHANGED, () => {
            this.videoEl.dispatchEvent(new Event('volumechange'));
        });

        addRemoteListener(cast.framework.RemotePlayerEventType.DURATION_CHANGED, () => {
            this.videoEl.dispatchEvent(new Event('durationchange'));
            this.videoEl.dispatchEvent(new Event('loadedmetadata'));
        });

        addRemoteListener(cast.framework.RemotePlayerEventType.CURRENT_TIME_CHANGED, () => {
            this.videoEl.dispatchEvent(new Event('timeupdate'));
            if (this.currentTime >= this.duration - 0.5 && this.duration > 0) {
                if (!this.endedMedia) {
                    this.endedMedia = true;
                    this.videoEl.dispatchEvent(new Event('ended'));
                }
            }
        });

        // Initialize and load the media on Chromecast
        this.load();
    }

    destroy() {
        // Restore original methods on videoEl
        this.videoEl.play = this.originalPlay;
        this.videoEl.pause = this.originalPause;
        this.videoEl.load = this.originalLoad;

        // Restore original properties on videoEl
        delete this.videoEl.paused;
        delete this.videoEl.currentTime;
        delete this.videoEl.volume;
        delete this.videoEl.muted;
        delete this.videoEl.duration;

        // Unsubscribe all RemotePlayer listeners
        for (const { type, handler } of this.listeners) {
            this.controller.removeEventListener(type, handler);
        }

        // Restore local video playback volume / muted state if needed
        this.videoEl.muted = false;
    }

    get paused() {
        return this.player.isPaused;
    }

    get currentTime() {
        return this.player.currentTime;
    }

    set currentTime(value) {
        this.player.currentTime = value;
        this.controller.seek();
        this.videoEl.dispatchEvent(new Event('timeupdate'));
    }

    get duration() {
        return this.player.duration;
    }

    get volume() {
        return this.player.volumeLevel;
    }

    set volume(value) {
        this.player.volumeLevel = value;
        this.controller.setVolumeLevel();
        this.videoEl.dispatchEvent(new Event('volumechange'));
    }

    get muted() {
        return this.player.isMuted;
    }

    set muted(value) {
        if (value !== this.player.isMuted) {
            this.controller.muteOrUnmute();
            this.videoEl.dispatchEvent(new Event('volumechange'));
        }
    }

    play() {
        if (this.player.isPaused) {
            this.controller.playOrPause();
            this.videoEl.dispatchEvent(new Event('play'));
        }
    }

    pause() {
        if (!this.player.isPaused) {
            this.controller.playOrPause();
            this.videoEl.dispatchEvent(new Event('pause'));
        }
    }

    load() {
        const castSession = cast.framework.CastContext.getInstance().getCurrentSession();
        if (!castSession) return;

        // Get media URL from video element source tags or src attribute
        const sourceEl = this.videoEl.querySelector('source');
        const url = sourceEl ? sourceEl.src : this.videoEl.src;
        if (!url || url === window.location.href) return;

        // Find type
        let type = 'video/mp4';
        const ext = url.split('?')[0].split('.').pop().toLowerCase();
        if (ext === 'm3u8') {
            type = 'application/x-mpegURL';
        }

        const mediaInfo = new chrome.cast.media.MediaInfo(url, type);
        mediaInfo.metadata = new chrome.cast.media.GenericMediaMetadata();
        mediaInfo.streamType = chrome.cast.media.StreamType.BUFFERED;

        // Set title and subtitle from video attributes if present
        const title = this.videoEl.getAttribute('data-cast-title') || document.title;
        if (title) mediaInfo.metadata.title = title;

        const poster = this.videoEl.getAttribute('poster');
        if (poster) {
            const absolutePoster = new URL(poster, window.location.href).href;
            mediaInfo.metadata.images = [{ url: absolutePoster }];
        }

        const request = new chrome.cast.media.LoadRequest(mediaInfo);
        
        // Match the current local position
        const localCurrentTime = this.videoEl.currentTime || 0;

        castSession.loadMedia(request).then(() => {
            if (localCurrentTime > 0) {
                this.currentTime = localCurrentTime;
            }
            this.play();
        }, (err) => {
            console.error('Failed to load media on Chromecast:', err);
        });
    }
}

function setupCastSessionListener(ctx) {
    if (isSessionListenerAdded) return;
    isSessionListenerAdded = true;

    ctx.addEventListener(cast.framework.CastContextEventType.SESSION_STATE_CHANGED, (event) => {
        switch (event.sessionState) {
            case cast.framework.SessionState.SESSION_STARTED:
            case cast.framework.SessionState.SESSION_RESUMED: {
                const castSession = ctx.getCurrentSession();
                if (castSession && currentVideoEl) {
                    const player = new cast.framework.RemotePlayer();
                    const controller = new cast.framework.RemotePlayerController(player);
                    if (activeCastPlayer) {
                        activeCastPlayer.destroy();
                    }
                    activeCastPlayer = new ChromecastPlayer(player, controller, currentVideoEl);
                }
                break;
            }
            case cast.framework.SessionState.SESSION_ENDED:
                if (activeCastPlayer) {
                    activeCastPlayer.destroy();
                    activeCastPlayer = null;
                }
                break;
        }
    });
}

export function initChromecast(videoEl, containerEl) {
    currentVideoEl = videoEl;

    function initCast() {
        if (!window.cast || !window.chrome?.cast) return;
        const ctx = cast.framework.CastContext.getInstance();
        ctx.setOptions({
            receiverApplicationId: chrome.cast.media.DEFAULT_MEDIA_RECEIVER_APP_ID,
            autoJoinPolicy: chrome.cast.AutoJoinPolicy.PAGE_SCOPED,
            androidReceiverCompatible: true,
        });

        // Mount cast launcher
        if (containerEl && !containerEl.querySelector('google-cast-launcher')) {
            const launcher = document.createElement('google-cast-launcher');
            containerEl.appendChild(launcher);
        }

        // Setup session state listener
        setupCastSessionListener(ctx);
    }

    if (window.cast) {
        initCast();
    } else {
        window.__onGCastApiAvailable = (available) => { if (available) initCast(); };
        if (!document.querySelector('script[src*="cast_sender.js"]')) {
            const s = document.createElement('script');
            s.src = 'https://www.gstatic.com/cv/js/sender/v1/cast_sender.js?loadCastFramework=1';
            document.body.appendChild(s);
        }
    }
}
