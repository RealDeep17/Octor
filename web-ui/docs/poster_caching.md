# Backend Poster & Still Image Caching Pipeline

This document describes the Go backend architecture for serving and caching resized library posters and series episode still images.

---

## 1. The Cache-Miss Bottleneck

Previously, when a torrent was loaded for the first time via a magnet link or manually re-enriched, the backend S3 poster and episode still cache was empty.

When a client requested `/lib/:type/poster/...` or `/lib/episode/still/...`, the Go backend synchronously:
1. Made a synchronous HTTP call to retrieve the original high-resolution poster from external CDNs (TMDB, TPDB, StashDB, etc.).
2. Blocked on slow network latency (frequently 5–15 seconds on foreign or adult CDNs).
3. Decoded the large original image, resized it via `imaging.Resize`, and encoded it back to JPEG on the main thread (heavy CPU usage).
4. Uploaded the resized JPEG to S3.
5. Finally responded with the image bytes.

This caused massive page freeze delays, gateway timeouts, and server-side CPU starvation when multiple images (such as entire television seasons) were requested in parallel.

---

## 2. Non-Blocking Redirect & Background Caching Architecture

To resolve the delay, the image handlers use a decoupled, asynchronous model:

```
[Browser]
    │  1. GET /lib/movie/poster/tt12345/480.jpg
    ▼
[poster.go / still.go]
    │  2. S3 Cache GetObject (Timeout 1s)
    │       ├──► [Hit]  Serve cached resized JPEG immediately (<10ms)
    │       └──► [Miss] Fall through
    │
    │  3. Local Database Metadata Lookup (Fast query <10ms)
    │       ├──► Resolve original CDN URL (horizontal/vertical, normalized)
    │       └──► Not found -> Return 404 / 500 error
    │
    │  4. Spawn Background Goroutine (Detached Context):
    │       ├──► Download original high-res image
    │       ├──► Decode & imaging.Resize in background
    │       └──► Upload resized JPEG to S3 cache (putPosterToCache)
    │
    ▼  5. Immediately respond with 302 Temporary Redirect to CDN URL (<15ms!)
[Browser follows redirect and renders image instantly from CDN edges]
```

### Key Design Benefits
*   **0ms Client Wait**: Decouples client rendering from backend downloading and resizing.
*   **Edge CDN Delivery**: Browsers fetch original images directly from highly distributed global CDNs, saving server bandwidth and matching the client's geographical location.
*   **Automatic Cache Warming**: Subsequent requests automatically hit the warmed S3 cache and are served directly with `200 OK` in under 10ms.
*   **Detached Context Safety**: The background resizing task uses `context.Background()` with a detached timeout context, ensuring S3 uploads complete even if the client terminates the browser connection.

---

## 3. Endpoints & Code Structure

### Routes
In [handler.go](file:///srv/octor/web-ui/handlers/library/handler.go):
```go
plg.GET("/:type/poster/:imdb_id/:file", h.poster)
plg.GET("/:type/poster-h/:imdb_id/:file", h.posterHorizontal)
plg.GET("/episode/still/:video_id/:season/:episode/:file", h.still)
```

### Handlers & Files
*   **[poster.go](file:///srv/octor/web-ui/handlers/library/poster.go)**:
    *   `handlePoster`: Core handler orchestrating S3 checks, CDN URL extraction, background routine spawning, and 302 redirects.
    *   `getOriginalPosterURL`: Resolves vertical/horizontal CDN URLs and normalizes porndb CDN signature segments.
    *   `getResizedJPEGPoster`: Performs background downloads, decoding, and linear resizing.
*   **[still.go](file:///srv/octor/web-ui/handlers/library/still.go)**:
    *   `still`: Core handler for episode still images.
    *   `getOriginalStillURL`: Queries Postgres episode metadata for the still URL.
    *   `getResizedJPEGStill`: Performs background downloading, decoding, and linear resizing.

---

## 4. Development Environment Fallback

If S3 cache is disabled or not configured in local development environments (`s3Cl == nil` or `posterCacheS3Bucket == ""`), the handlers automatically skip the background goroutine and directly perform a `302` client redirect to the external CDN URL. 

This ensures that developers experience instantaneous image loading on their local machines without needing S3 buckets or local cache setups.
