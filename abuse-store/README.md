# abuse-store

DMCA / abuse-notice service for the [Octor](https://github.com/webtor-io) platform.

It does three things behind a single gRPC API:

- **Stoplist for illegal content.** Reports with `cause = ILLEGAL_CONTENT` are persisted, and other services (`web-ui`, `torrent-http-proxy`) call `Check(infohash)` before serving content to block playback of reported torrents.
- **Mail relay.** Every accepted notice (illegal content, malware, app error, generic question) generates a support email and, when the reporter provided a return address, an acknowledgement.
- **Cleanup fan-out via NATS.** On every `ILLEGAL_CONTENT` report (new *and* duplicate) it publishes `resource.banned` so `web-ui` and `vault` can purge data tied to the banned infohash.

## Architecture

```
gRPC client ──► Push / Check  ─┬─► PostgreSQL  (source of truth, persisted abuses)
                               ├─► BadgerDB    (in-memory hot cache, rebuilt from PG on boot)
                               ├─► Mailer ──► SMTP ──► support + reporter
                               └─► NATS  ──► resource.banned ──► web-ui / vault cleanup
```

- **PostgreSQL** stores the canonical abuse records (`migrations/1_abuse.up.sql`).
- **BadgerDB** at `/tmp/badger` holds an infohash-keyed cache used by `Check`. It is loaded from the full `abuse` table on startup (`Store.Sync`) and refreshed every `--sync-interval` minutes — this is the only mechanism that propagates inserts between replicas, so don't expect sub-second cross-pod consistency.
- **SMTP** delivery is best-effort and runs in goroutines; failures are logged but don't fail the RPC.

## gRPC API

Defined in [`proto/abuse-store.proto`](proto/abuse-store.proto):

- `Push(PushRequest) → PushReply` — submit an abuse notice. `infohash` and `email` are validated; `notice_id` and `started_at` default to a fresh UUID and the current time. Only `ILLEGAL_CONTENT` causes a database write; duplicates by infohash are rejected with `ALREADY_EXISTS` but **still publish `resource.banned`** (recovery for a dropped first publish).
- `Check(CheckRequest) → CheckReply` — returns `exists = true` if the infohash is in the stoplist (Badger lookup).

## NATS events

`ILLEGAL_CONTENT` reports publish to subject **`resource.banned`** with body:

```json
{ "infohash": "<lowercase hex>" }
```

The platform's JetStream stream `common` captures `resource.*`, so any consumer in the cluster can subscribe via a durable pull consumer. Existing subscribers:

- `web-ui` — drops the resource from `library`, `watch_history`, `cache_index`, `torrent_resource`, media metadata and the `vault.pledge` / `vault.tx_log` / `vault.resource` tables (refunding VP).
- `vault` — queues the resource for deletion (S3 + own DB) via `ResourceQueueForDeletion`.

Publishing is best-effort and never fails the RPC. Consumers must be idempotent — duplicate reports re-fire the event by design, which lets a re-submitted form recover from a transient NATS outage.

## Build & run

```bash
go build ./...                           # main server binary + ./client CLI
go run . serve [flags...]                # start the server
go run . migrate up                      # apply PostgreSQL migrations
make protoc                              # regenerate proto/*.pb.go
```

| `--grpc-port`        | `GRPC_PORT`             | `50051`   | gRPC listening port (Octor default: `50059`) |
| `--pprof-port`       | `PPROF_PORT`            | `8080`    | pprof listening port (Octor default: `51059`) |
| `--probe-port`       | `PROBE_PORT`            | `8081`    | probe listening port (Octor default: `52059`) |
| `--sync-interval`    | `STORE_SYNC_INTERVAL`   | `10`      | PG → Badger resync interval, minutes         |
| `--smtp-host`        | `SMTP_HOST`             | `""`      | SMTP host                                    |
| `--smtp-port`        | `SMTP_PORT`             | `0`       | SMTP port                                    |
| `--smtp-user`        | `SMTP_USER`             | `""`      | SMTP user                                    |
| `--smtp-pass`        | `SMTP_PASS`             | `""`      | SMTP pass                                    |
| `--smtp-tls`         | `SMTP_TLS`              | `false`   | use implicit TLS (port 465 style)            |
| `--smtp-start-tls`   | `SMTP_STARTTLS`         | `false`   | use STARTTLS                                 |
| `--smtp-tls-secure`  | `SMTP_TLS_SECURE`       | `true`    | verify TLS certificates                      |
| `--mail-sender`      | `MAIL_SENDER`           | `noreply@octor` | mail sender                          |
| `--mail-support`     | `MAIL_SUPPORT`          | `support@octor` | mail support                          |

*Postgres and NATS options are provided by `common-services`.*

### Standard Octor Ports

When managed by `run.sh`, the following ports are used:
- **gRPC:** `50059`
- **Pprof:** `51059`
- **Probe:** `52059`

## gRPC API

Defined in [`proto/abuse-store.proto`](proto/abuse-store.proto):

- `Push(PushRequest) → PushReply` — submit an abuse notice. `infohash` and `email` are validated; `notice_id` and `started_at` default to a fresh UUID and the current time. Only `ILLEGAL_CONTENT` causes a database write; duplicates by infohash are rejected with `ALREADY_EXISTS` but **still publish `resource.banned`** (recovery for a dropped first publish).
- `Check(CheckRequest) → CheckReply` — returns `exists = true` if the infohash is in the stoplist (Badger lookup).

## NATS events

`ILLEGAL_CONTENT` reports publish to subject **`resource.banned`** with body:

```json
{ "infohash": "<lowercase hex>" }
```

The platform's JetStream stream `common` captures `resource.*`, so any consumer in the cluster can subscribe via a durable pull consumer. Existing subscribers:

- `web-ui` — drops the resource from `library`, `watch_history`, `cache_index`, `torrent_resource`, media metadata and the `vault.pledge` / `vault.tx_log` / `vault.resource` tables (refunding VP).
- `vault` — queues the resource for deletion (S3 + own DB) via `ResourceQueueForDeletion`.

Publishing is best-effort and never fails the RPC. Consumers must be idempotent — duplicate reports re-fire the event by design, which lets a re-submitted form recover from a transient NATS outage.

## Build & run

```bash
go build ./...                           # main server binary + ./client CLI
go run . serve [flags...]                # start the server
go run . migrate up                      # apply PostgreSQL migrations
make protoc                              # regenerate proto/*.pb.go
```

### Client CLI

The `client/` directory contains a small `urfave/cli` tool for smoke-testing:

```bash
go run ./client --host 127.0.0.1 --port 50059 push \
    --hash <infohash> --work "..." --email reporter@example.com --description "..."

go run ./client --host 127.0.0.1 --port 50059 check --hash <infohash>
```

## Migrations

SQL files in [`migrations/`](migrations/), numbered `N_name.{up,down}.sql`, run by the `migrate` subcommand from the `common-services` package. The Docker image copies the directory to `/migrations` so migrations are available at runtime.

## Docker

```bash
docker build -t octor/abuse-store .
docker run --rm -p 50059:50059 -p 8081:8081 \
    -e PG_HOST=... -e PG_PORT=5433 -e PG_USER=... -e PG_PASSWORD=... -e PG_DATABASE=... \
    -e SMTP_HOST=... -e SMTP_PORT=587 -e SMTP_USER=... -e SMTP_PASS=... -e SMTP_STARTTLS=true \
    octor/abuse-store
```

Built and pushed to `ghcr.io/webtor-io/abuse-store` by [`.github/workflows/docker-image.yml`](.github/workflows/docker-image.yml) on push to `main` and on `v*` tags.

## License

[MIT](LICENSE)
