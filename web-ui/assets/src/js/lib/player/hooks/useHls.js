import { useEffect, useRef } from 'preact/hooks';
import { createHls, initDefaultTracks, Hls } from '../hls-manager';

/**
 * Hook that manages HLS.js lifecycle.
 * Attaches to videoRef, returns hlsRef for external access (track control, etc.).
 */
export function useHls(videoRef, sourceUrl, { onReady } = {}) {
    const hlsRef = useRef(null);
    const tracksInitialized = useRef(false);

    useEffect(() => {
        const video = videoRef.current;
        if (!video || !sourceUrl) return;

        // Check if source is HLS
        const isHls = sourceUrl.includes('.m3u8') || sourceUrl.includes('mpegurl');

        if (isHls) {
            const hls = createHls(video, sourceUrl, () => {
                if (onReady) onReady(hls);
            });

            if (hls) {
                hlsRef.current = hls;
                window.hlsPlayer = hls;

                // Init default tracks on first canplay
                const onCanPlay = () => {
                    if (!tracksInitialized.current && hls) {
                        tracksInitialized.current = true;
                        initDefaultTracks(hls);
                    }
                };
                video.addEventListener('canplay', onCanPlay);

                // Lower quality during seek
                const onSeeking = () => {
                    if (hls.loadLevel > 1) hls.loadLevel = 1;
                };
                const onSeeked = () => {
                    hls.loadLevel = -1;
                };
                video.addEventListener('seeking', onSeeking);
                video.addEventListener('seeked', onSeeked);

                const onPause = () => hls.stopLoad();
                const onEnded = () => hls.stopLoad();
                const onPlay = () => hls.startLoad();
                video.addEventListener('pause', onPause);
                video.addEventListener('ended', onEnded);
                video.addEventListener('play', onPlay);

                return () => {
                    video.removeEventListener('canplay', onCanPlay);
                    video.removeEventListener('seeking', onSeeking);
                    video.removeEventListener('seeked', onSeeked);
                    video.removeEventListener('pause', onPause);
                    video.removeEventListener('ended', onEnded);
                    video.removeEventListener('play', onPlay);
                    hls.stopLoad();
                    hls.destroy();
                    hlsRef.current = null;
                    window.hlsPlayer = null;
                };
            } else {
                // Native HLS (Safari) — source already set in createHls
                return;
            }
        } else {
            // Non-HLS source (direct file)
            video.src = sourceUrl;
        }
    }, [videoRef, sourceUrl]);

    return hlsRef;
}
