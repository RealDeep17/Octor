# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Service role

`abuse-store` is the DMCA / abuse-notice sink for the Webtor platform. It does three distinct jobs that share one gRPC surface:

1. **Stoplist** for torrents reported as illegal content. Other components (web-ui, torrent-http-proxy) call `Check(infohash)` before serving content; a positive answer blocks playback.
2. **Mail relay** for every kind of incoming notice (illegal content, malware, app error, generic question), forwarding to support and acknowledging the reporter.
3. **Cleanup fan-out** via NATS: on `Cause = ILLEGAL_CONTENT` it publishes `resource.banned` so downstream services (web-ui, vault) can purge their data for that infohash.

Only `Cause = ILLEGAL_CONTENT` notices are persisted in the stoplist or fan out via NATS. Other causes (`MALWARE`, `APP_ERROR`, `QUESTION`) are passed through to email only — see `services/grpc.go` `Push`. Don't add `store.Push` calls for non-illegal causes without understanding this split.

## Build & run

```bash
go build ./...                 # binary `abuse-store` (server) + `client/` CLI
go vet ./...
go run . serve [flags...]      # run the server locally
go run . migrate up            # run PG migrations (uses common-services migration CMD)
go run ./client push --hash ... --work ... --email ...   # smoke-test the gRPC API
make protoc                    # regenerate proto/abuse-store.pb.go + _grpc.pb.go
```

There are no tests in this repo.

The `Dockerfile` produces a static linux binary, copies `migrations/` into `/migrations`, and exposes `8081` (probes) + `50051` (gRPC). HTTP serving is *not* exposed (the service has no HTTP handlers).

## Architecture

### Two-tier storage, async sync

The store has a non-obvious dual layout:

- **PostgreSQL** (`models.Abuse` via `go-pg`) — source of truth, persistent.
- **BadgerDB** at `/tmp/badger` — keyed by infohash, in-memory hot cache used only by the `Check` RPC.

Lifecycle (see `services/store.go`):
- On startup, `Store.Sync()` iterates **the whole `abuse` table** and pushes every row into Badger. This is intentional — the entire stoplist is loaded on boot so `Check` is O(1) on a local KV with no PG hop.
- `Push` writes to PG first, then mirrors into Badger (`pushToCache`).
- `Store.Serve()` re-runs `Sync()` every `STORE_SYNC_INTERVAL` minutes (default 10) to pick up rows inserted by other replicas. **This is the only cross-replica consistency mechanism** — there is no pub/sub.
- Badger lives on the pod's local filesystem at `/tmp/badger`; it is treated as ephemeral cache and rebuilt from PG on every restart.

Implication: if you add a write path, write to PG first and let `pushToCache` mirror it. Do not add Badger-only writes — other replicas will not see them until the next sync tick.

### gRPC entry point

`services/grpc.go` implements `pb.AbuseStoreServer`:

- `Push` validates `infohash` (`[0-9a-f]{5,40}` regex — lenient, partial hashes accepted) and `email`, fills defaults (`notice_id` → uuid, `subject` → `Abuse notice <id>`, `started_at` → now), and:
  - For `ILLEGAL_CONTENT`: calls `Check`; persists only if not already present; **publishes `resource.banned` to NATS regardless** (new insert *and* duplicate report); returns `AlreadyExists` on duplicate.
  - For other causes: skips persistence and NATS entirely.
  - Fires `SendUserEmail` (only if reporter supplied an email) and `SendSupportEmail` in goroutines — failures are logged, not returned. Mail delivery is best-effort and not part of the RPC's success contract.
- `Check` reads only Badger.

### NATS fan-out (`resource.banned`)

`publishBanned` in `services/grpc.go` publishes `{"infohash":"..."}` to subject `resource.banned` (JetStream stream `common`, declared in `infra/helmfile/values/streams.yaml.gotmpl` with subject filter `resource.*`).

Two non-obvious choices:

- **Always publish on `ILLEGAL_CONTENT`, even on duplicate.** A repeat report from the form re-triggers downstream cleanup, recovering from a previously dropped publish (e.g. NATS unreachable on the first attempt). At-least-once semantics; consumers must be idempotent.
- **Best-effort, not RPC-blocking.** Marshal/connect/publish errors are logged but never fail the RPC — mirrors the mailer's behaviour. There is no outbox: if NATS is down and the reporter never re-submits, the event is lost. This is acceptable because the stoplist itself (Badger/PG) is the authoritative cut-off; downstream cleanup is housekeeping.

`cs.NATS` is created from `--nats-service-host` / `--nats-service-port` (env vars `NATS_SERVICE_HOST/PORT` are auto-injected by Kubernetes from the `nats` Service in the `webtor` namespace; no chart wiring required). If the host flag is empty, `NewNATS` returns `nil` and `publishBanned` becomes a no-op — useful for local runs without NATS.

### Mailer

`services/mailer.go` renders two embedded templates (`services/templates/{user,support}.go.tpl`) — `text/template`, plain-text email body, headers prepended manually in `services/smtp.go`. The user template branches on `IsIllegal` to send either a "added to stoplist" or "we'll look into it" body.

`services/smtp.go` handles three connection modes selected by flags:
- `SMTP_TLS=true` → implicit TLS via `tls.Dial` (port 465 style).
- `SMTP_TLS=false, SMTP_STARTTLS=true` → plaintext dial, then `STARTTLS`.
- Both false → plaintext.
- `SMTP_TLS_SECURE` (default true) controls cert verification; set false only for self-signed dev SMTP.

### Wiring

`serve.go` is the canonical wiring read for this service: PG → migrations → Badger → Store (with initial Sync) → SMTP → Mailer → Probe → GRPC → `cs.NewServe(probe, grpc, store)`. The `serve` runner from `common-services` runs `Probe`, `GRPC`, and `Store.Serve()` (the periodic-sync ticker) concurrently. Adding a new long-running component means passing it into `cs.NewServe(...)`; otherwise it never starts.

## Conventions inherited from the platform

- CLI flags via `urfave/cli` v1, env-var fallbacks via `EnvVar`. Flag registration is split into `Register*Flags` helpers per service file — keep this pattern.
- Common infra (PG, migrations, Probe, Serve) comes from `github.com/webtor-io/common-services`; do not reimplement.
- Errors wrapped with `github.com/pkg/errors`; logs via `logrus` with `WithError` / `WithField`.
- Migrations are SQL-only files under `migrations/`, numbered `N_name.{up,down}.sql`. The Dockerfile copies the directory into `/migrations` so the `migrate` subcommand finds them at runtime.

## Cross-service touchpoints

Callers in this monorepo: `web-ui` (`services/abuse_store/client.go` — DMCA form + Check on playback). The proto contract in `proto/abuse-store.proto` is the public surface; bumping field numbers or removing enum values is a breaking change for every caller.
