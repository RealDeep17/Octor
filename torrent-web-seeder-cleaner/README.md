# torrent-web-seeder-cleaner

Deletes torrent-web-seeder media cache from `DATA_DIR`.

By default it removes inactive cache entries older than `23h`, keeping media
cache lifetime under one day, and still performs the existing free-space cleanup.

Useful flags/env:

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--max-age` | `CACHED_MEDIA_MAX_AGE`, `CLEANER_MAX_AGE` | `23h` | Maximum inactive media cache age; set `0` to disable age cleanup |
| `--interval` | `CACHED_MEDIA_CLEAN_INTERVAL`, `CLEANER_INTERVAL` | `5m` | Cleanup interval |
| `--data-dir` | `DATA_DIR` | system temp | Seeder media cache directory |
| `--keep-free` | `CLEANER_KEEP_FREE` | `25%` | Start space cleanup below this free-space threshold |
| `--free` | `CLEANER_FREE` | `35%` | Free up to this threshold during space cleanup |
