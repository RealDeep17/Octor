# common-services

A collection of shared Go packages used across the Octor microservices platform. It provides standardized implementations for infrastructure connectivity, health monitoring, and service orchestration.

## 📦 Packages

### `cs.Serve`
The primary orchestrator for running multiple goroutine-based services (like HTTP, gRPC, and workers) simultaneously. It handles graceful shutdown and error propagation.

### `cs.Probe`
Standardized Kubernetes-compatible health probes (`/liveness`, `/readiness`).
- **Default Port:** `8081`

### `cs.PG`
PostgreSQL connection management using `go-pg`. Supports connection pooling and environment-based configuration.
- **Flags:** `--postgres-host`, `--postgres-port`, `--postgres-user`, `--postgres-password`, `--postgres-database`

### `cs.NATS`
NATS connectivity helper.
- **Flags:** `--nats-service-host`, `--nats-service-port`

### `cs.Redis`
Redis client initialization.
- **Flags:** `--redis-host`, `--redis-port`

### `cs.S3`
AWS S3 / Minio client wrapper.
- **Flags:** `--aws-endpoint`, `--aws-region`, `--aws-access-key-id`, `--aws-secret-access-key`, `--aws-bucket`, `--aws-no-ssl`

### `cs.Pprof` & `cs.Prom`
Standardized endpoints for profiling and Prometheus metrics.
- **Pprof Default Port:** `8082`
- **Prom Default Port:** `8083`

## 🛠 Example Usage

```go
package main

import (
    cs "github.com/webtor-io/common-services"
    log "github.com/sirupsen/logrus"
    "github.com/urfave/cli"
)

func main() {
    app := cli.NewApp()
    app.Flags = cs.RegisterProbeFlags([]cli.Flag{})
    app.Action = func(c *cli.Context) error {
        probe := cs.NewProbe(c)
        defer probe.Close()

        serve := cs.NewServe(probe)
        return serve.Serve()
    }
    app.Run(os.Args)
}
```

## ⚖️ License

All rights reserved.
