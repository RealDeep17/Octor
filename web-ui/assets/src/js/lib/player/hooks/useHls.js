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

                const onEnded = () => hls.stopLoad();
                video.addEventListener('ended', onEnded);

                return () => {
                    video.removeEventListener('canplay', onCanPlay);
                    video.removeEventListener('ended', onEnded);
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
