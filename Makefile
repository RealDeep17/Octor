.PHONY: all build build-go build-web-ui build-sidecar clean systemd push deploy

# Resolve BIN_DIR dynamically — reads from custom.env if present, else falls back to ./bin
# This mirrors the logic in run.sh so `make` and `./run.sh build` always write to the same place.
PROJECT_ROOT := $(shell pwd)
CUSTOM_ENV   := $(PROJECT_ROOT)/custom.env
BIN_DIR      := $(shell grep -s '^BIN_DIR=' $(CUSTOM_ENV) | cut -d= -f2- | tr -d '\r' || echo "$(PROJECT_ROOT)/bin")
ifeq ($(strip $(BIN_DIR)),)
  BIN_DIR := $(PROJECT_ROOT)/bin
endif

# Remote deploy target — reads DEPLOY_HOST from custom.env (e.g. ubuntu@1.2.3.4)
# Set DEPLOY_HOST=user@host and DEPLOY_PATH=/home/ubuntu/octor in custom.env
DEPLOY_HOST := $(shell grep -s '^DEPLOY_HOST=' $(CUSTOM_ENV) | cut -d= -f2- | tr -d '\r')
DEPLOY_PATH := $(shell grep -s '^DEPLOY_PATH=' $(CUSTOM_ENV) | cut -d= -f2- | tr -d '\r' || echo "/home/ubuntu/octor")

SERVICES := rest-api web-ui vault abuse-store claims-provider torrent-store url-store video-info torrent-archiver srt2vtt content-transcoder magnet2torrent torrent-web-seeder content-prober torrent-http-proxy torrent-web-seeder-cleaner s3-gateway sidecar ai-proxy create_nats_stream recover_db clean_orphans

all: build

build: build-go build-web-ui

build-go:
	@mkdir -p $(BIN_DIR)
	@echo "🔨 Building unified dispatcher binary..."
	@go run scripts/build_unified.go || exit 1
	@for svc in $(SERVICES); do \
		echo "🔗 Linking $$svc to octor..."; \
		ln -sf octor $(BIN_DIR)/$$svc || exit 1; \
	done
	@echo "✅ All Go services and helper scripts built successfully in $(BIN_DIR) as unified binaries"

build-web-ui:
	@echo "📦 Setting up Web UI node_modules and building assets..."
	@cd web-ui && npm install && npm run build
	@echo "✅ Web UI assets built successfully"

build-sidecar:
	@echo "🚀 Go rewrite of sidecar is now built automatically via build-go"

systemd:
	@echo "Synchronizing systemd service files..."
	@sudo cp $(PROJECT_ROOT)/deploy/systemd/octor-*.service /etc/systemd/system/
	@sudo systemctl daemon-reload
	@echo "Systemd service files synchronized"

push: ## Rsync local project to remote server (set DEPLOY_HOST and DEPLOY_PATH in custom.env)
	@if [ -z "$(DEPLOY_HOST)" ]; then \
		echo "ERROR: DEPLOY_HOST not set in custom.env"; \
		echo "Add: DEPLOY_HOST=ubuntu@your-server-ip"; \
		echo "Add: DEPLOY_PATH=/home/ubuntu/octor  (optional, defaults to /home/ubuntu/octor)"; \
		exit 1; \
	fi
	@echo "Pushing to $(DEPLOY_HOST):$(DEPLOY_PATH) ..."
	@rsync -avz --progress \
		--exclude='.git/' \
		--exclude='infra-data/' \
		--exclude='scratch/' \
		--exclude='bin/' \
		--exclude='web-ui/node_modules/' \
		--exclude='web-ui/assets/dist/' \
		--exclude='sidecar/venv/' \
		--exclude='custom.env' \
		--exclude='*.log' \
		$(PROJECT_ROOT)/ $(DEPLOY_HOST):$(DEPLOY_PATH)/
	@echo "Push complete. Run on server: cd $(DEPLOY_PATH) && ./run.sh build && ./run.sh mode"

deploy: push ## Alias for push

clean:
	@echo "🧹 Cleaning binaries and temporary files..."
	@rm -rf $(BIN_DIR)
	@rm -rf web-ui/node_modules web-ui/assets/dist
	@rm -rf sidecar/venv
	@echo "✨ Clean completed"

