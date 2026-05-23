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

The service listens on gRPC port **50051**.

## Configuration

Primary configuration is stored in `custom.env`.

- **gRPC Port:** 50051
- **Redis:** `REDIS_HOST`, `REDIS_PORT=6380`
- **S3 Gateway:** `AWS_ENDPOINT=http://localhost:9000`, `AWS_BUCKET=torrent-store`

Example `custom.env`:
```env
GRPC_PORT=50051
REDIS_PORT=6380
AWS_ENDPOINT=http://localhost:9000
```

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
