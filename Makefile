.PHONY: all build build-go build-web-ui build-sidecar clean

BIN_DIR := /srv/octor/bin
SERVICES := rest-api web-ui vault abuse-store claims-provider torrent-store url-store video-info torrent-archiver srt2vtt content-transcoder magnet2torrent torrent-web-seeder content-prober torrent-http-proxy torrent-web-seeder-cleaner s3-gateway

all: build

build: build-go build-web-ui build-sidecar

build-go:
	@mkdir -p $(BIN_DIR)
	@for svc in $(SERVICES); do \
		echo "🔨 Building Go service: $$svc..."; \
		if [ -d "$$svc/server" ]; then \
			cd "$$svc/server" && go build -o $(BIN_DIR)/$$svc . ; \
		else \
			cd "$$svc" && go build -o $(BIN_DIR)/$$svc . ; \
		fi; \
		cd - > /dev/null; \
	done
	@echo "✅ All Go services built successfully in $(BIN_DIR)"

build-web-ui:
	@echo "📦 Setting up Web UI node_modules and building assets..."
	@cd web-ui && npm install && npm run build
	@echo "✅ Web UI assets built successfully"

build-sidecar:
	@echo "🐍 Setting up Python sidecar virtualenv..."
	@cd sidecar && python3 -m venv venv && ./venv/bin/pip install -r requirements.txt
	@echo "✅ Python sidecar setup successfully"

clean:
	@echo "🧹 Cleaning binaries and temporary files..."
	@rm -rf $(BIN_DIR)
	@rm -rf web-ui/node_modules web-ui/assets/dist
	@rm -rf sidecar/venv
	@echo "✨ Clean completed"
