#!/bin/bash
set -e

# Define all services and their unit file contents
declare -A SERVICES

SERVICES[octor-ai-proxy]="[Unit]
Description=Octor AI Proxy
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor
EnvironmentFile=/srv/octor/custom.env
ExecStart=/usr/bin/node ai-proxy/proxy.js
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-sidecar]="[Unit]
Description=Octor Sidecar
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/sidecar
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/sidecar/venv/bin/uvicorn main:app --host 0.0.0.0 --port 8000
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-rest-api]="[Unit]
Description=Octor REST API
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/rest-api
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/rest-api serve --port 8080 --pprof-port 51080 --probe-port 52080 --export-domain \${OCTOR_DOMAIN} --torrent-store-host 127.0.0.1 --torrent-store-port 50051 --magnet2torrent-host 127.0.0.1 --magnet2torrent-port 50053 --video-info-host 127.0.0.1 --video-info-port 50056 --export-use-subdomains false
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-web-ui]="[Unit]
Description=Octor Web UI
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/web-ui
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/web-ui serve --port 8082 --pprof-port 51081 --probe-port 52081 --webtor-rest-api-host localhost --webtor-rest-api-port 8080 --vault-service-host localhost --vault-service-port 8086 --use-internal-torrent-http-proxy=true --torrent-http-proxy-host localhost --torrent-http-proxy-port 50052 --domain \${OCTOR_DOMAIN}
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-vault]="[Unit]
Description=Octor Vault
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/vault
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/vault serve --port 8086 --pprof-port 51086 --probe-port 52086 --prom-port 53086 --webtor-rest-api-host localhost --webtor-rest-api-port 8080 --aws-endpoint \${AWS_ENDPOINT} --aws-region \${AWS_REGION} --aws-access-key-id \${AWS_ACCESS_KEY_ID} --aws-secret-access-key \${AWS_SECRET_ACCESS_KEY} --aws-bucket \${VAULT_AWS_BUCKET} --aws-no-ssl --postgres-database vault
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-abuse-store]="[Unit]
Description=Octor Abuse Store
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/abuse-store
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/abuse-store serve --grpc-port 50059 --pprof-port 51059 --probe-port 52059 --postgres-database abuse_store
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-claims-provider]="[Unit]
Description=Octor Claims Provider
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/claims-provider
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/claims-provider serve --grpc-port 50060 --pprof-port 51060 --probe-port 52060 --prom-port 53060 --postgres-database claims_provider
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-torrent-store]="[Unit]
Description=Octor Torrent Store
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/torrent-store
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/torrent-store serve --grpc-port 50051 --pprof-port 51051 --probe-port 52051 --abuse-host 127.0.0.1 --abuse-port 50059 --use-abuse --use-s3 --aws-endpoint \${AWS_ENDPOINT} --aws-access-key-id \${AWS_ACCESS_KEY_ID} --aws-secret-access-key \${AWS_SECRET_ACCESS_KEY} --aws-bucket \${TORRENT_STORE_AWS_BUCKET} --aws-region \${AWS_REGION} --aws-no-ssl
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-magnet2torrent]="[Unit]
Description=Octor Magnet2Torrent
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/magnet2torrent/server
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/magnet2torrent --port 50053 --pprof-port 51053 --probe-port 52053
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-url-store]="[Unit]
Description=Octor URL Store
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/url-store
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/url-store serve --port 54061 --grpc-port 50061 --probe-port 52061 --postgres-database url_store
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-torrent-web-seeder]="[Unit]
Description=Octor Torrent Web Seeder
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/torrent-web-seeder/server
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/torrent-web-seeder --port 50054 --pprof-port 51054 --probe-port 52054 --prom-port 53054
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-torrent-web-seeder-cleaner]="[Unit]
Description=Octor Torrent Web Seeder Cleaner
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/torrent-web-seeder-cleaner
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/torrent-web-seeder-cleaner serve
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-content-transcoder]="[Unit]
Description=Octor Content Transcoder
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/content-transcoder
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/content-transcoder --port 50055 --pprof-port 51055 --probe-port 52055
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-video-info]="[Unit]
Description=Octor Video Info
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/video-info
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/video-info --port 50056 --probe-port 52056 --redis-host \${REDIS_HOST} --redis-port \${REDIS_PORT}
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-torrent-archiver]="[Unit]
Description=Octor Torrent Archiver
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/torrent-archiver
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/torrent-archiver --port 50057 --pprof-port 51057 --probe-port 52057 --torrent-store-host localhost --torrent-store-port 50051 --prom-port 53057
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-srt2vtt]="[Unit]
Description=Octor SRT2VTT
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/srt2vtt
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/srt2vtt --port 50058 --probe-port 52058
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-content-prober]="[Unit]
Description=Octor Content Prober
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/content-prober/server
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/content-prober --port 50063 --http-port 50062 --probe-port 52062 --redis-host \${REDIS_HOST} --redis-port \${REDIS_PORT}
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

SERVICES[octor-torrent-http-proxy]="[Unit]
Description=Octor Torrent HTTP Proxy
After=network.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/srv/octor/torrent-http-proxy
EnvironmentFile=/srv/octor/custom.env
ExecStart=/srv/octor/bin/torrent-http-proxy --port 50052 --torrent-http-proxy-host 127.0.0.1 --torrent-http-proxy-port 50052 --pprof-port 51052 --probe-port 52052 --config services.yaml
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target"

# Write service files using sudo
for name in "${!SERVICES[@]}"; do
    echo "   -> Writing /etc/systemd/system/${name}.service..."
    sudo tee "/etc/systemd/system/${name}.service" > /dev/null <<EOF
${SERVICES[$name]}
EOF
done

echo "🔄 Reloading systemd daemon..."
sudo systemctl daemon-reload

echo "🚀 Enabling all Octor services..."
for name in "${!SERVICES[@]}"; do
    sudo systemctl enable "${name}.service" > /dev/null 2>&1 || true
done

echo "🎉 Systemd services installed successfully!"
echo "To start all services, run: sudo systemctl start octor-*"
echo "To check status, run: sudo systemctl status octor-*"
