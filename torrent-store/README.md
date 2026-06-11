# torrent-store

Torrent store service with multiple backends and gRPC-access.

## Usage

The service is managed via `systemd` and configured using `custom.env`.

```bash
# Start the service
./run.sh mode production

# Check status
systemctl status octor-torrent-store
```

| Flag | Environment Variable | Default | Description |
|------|----------------------|---------|-------------|
| `--grpc-host` | `GRPC_HOST` | `""` | gRPC listening host |
| `--grpc-port` | `GRPC_PORT` | `50051` | gRPC listening port |
| `--pprof-port` | `PPROF_PORT` | `8080` | pprof listening port (Octor default: `51051`) |
| `--probe-port` | `PROBE_PORT` | `8081` | probe listening port (Octor default: `52051`) |

### Standard Octor Ports

When managed by `run.sh`, the following ports are used:
- **gRPC:** `50051`
- **Pprof:** `51051`
- **Probe:** `52051`

## Client usage

The client connects to the local server instance on `localhost:50051`.


```
% ./client help
NAME:
   torrent-store-client - interacts with torrent store

USAGE:
   client [global options] command [command options] [arguments...]

VERSION:
   0.0.1

COMMANDS:
   touch, to  touches torrent
   push, ps   pushes torrent to the store
   pull, pl   pulls torrent from the store
   help, h    Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --host value, -H value  hostname of the torrent store (default: "localhost") [$TORRENT_STORE_HOST]
   --port value, -P value  port of the torrent store (default: 50051) [$TORRENT_STORE_PORT]
   --help, -h              show help
   --version, -v           print the version
```
