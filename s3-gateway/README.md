# S3 Gateway

A lightweight, high-performance S3-compatible gateway written in Go. It is designed to sit in front of Rclone mounts (e.g., Google Drive, OneDrive) to provide a standard S3 interface with optimized sequential write buffering.

## 🚀 Purpose

Rclone VFS mounts can often struggle with random writes or small, frequent writes typical of some S3 clients. The Octor S3 Gateway solves this by:
- **Sequential Write Buffering:** Implementing a large memory-backed buffer for sequential writes.
- **Multipart Upload Support:** Efficiently handling large file uploads by staging parts locally before flushing them to the remote mount.
- **Zero-Leak Design:** Specifically tuned to ensure that file descriptors and temporary staging data are aggressively cleaned up.

## ⚙️ Configuration

The S3 Gateway is configured via environment variables, typically loaded from `custom.env`.

| Variable | Description | Default |
|----------|-------------|---------|
| `S3_GATEWAY_STORAGE_DIR` | The base directory where buckets are mapped. | `/srv/octor/infra-data/drive-mount` |
| `S3_GATEWAY_TEMP_UPLOADS_DIR` | Directory used to stage multipart upload parts. | `$STORAGE_DIR/.uploads` |
| `S3_GATEWAY_WRITE_BUFFER_SIZE` | Size of the sequential write buffer in bytes. | `16777216` (16MB) |
| `S3_GATEWAY_MAX_STAGING_SIZE` | Maximum total bytes allowed for staging parts. | `536870912` (512MB) |

## 🛠 Usage

The gateway listens on port **9000** by default. Any S3-compatible client (such as AWS CLI, MinIO Client, or the Octor Vault service) can point to it.

**Example Connection Details:**
- **Endpoint:** `http://localhost:9000`
- **Region:** `us-east-1` (ignored but required by most clients)
- **Access Key:** (Any, as it currently uses directory-based permissions)
- **Secret Key:** (Any)

## 🏗 Implementation Details

The gateway translates standard S3 REST API calls into local filesystem operations on the Rclone mount. It uses a `bufio.Writer` layer to ensure that data is flushed in large, sequential chunks, which is critical for maintaining high throughput on cloud-backed mounts.

## ⚖️ License

All rights reserved.
