ARG ALPINE_VER="3.21"
ARG NGINX_VERSION="1.29.3"
ARG CONTENT_TRANSCODER_COMMIT="bb121af0faa74aa6a962091b33527e4b24f8d9e4"
ARG VOD_MODULE_COMMIT="26f06877b0f2a2336e59cda93a3de18d7b23a3e2"
ARG SECURE_TOKEN_MODULE_COMMIT="24f7b99d9b665e11c92e585d6645ed6f45f7d310"

FROM alpine:$ALPINE_VER AS build-nginx-vod
ARG NGINX_VERSION
ARG VOD_MODULE_COMMIT
ARG SECURE_TOKEN_MODULE_COMMIT

RUN apk add --no-cache curl build-base openssl openssl-dev zlib-dev linux-headers pcre-dev &&     mkdir nginx nginx-vod-module nginx-secure-token-module &&     curl -sL https://nginx.org/download/nginx-${NGINX_VERSION}.tar.gz | tar -C /nginx --strip 1 -xz &&     curl -sL https://github.com/kaltura/nginx-vod-module/archive/${VOD_MODULE_COMMIT}.tar.gz | tar -C /nginx-vod-module --strip 1 -xz &&     curl -sL https://github.com/kaltura/nginx-secure-token-module/archive/${SECURE_TOKEN_MODULE_COMMIT}.tar.gz | tar -C /nginx-secure-token-module --strip 1 -xz

WORKDIR /nginx
RUN ./configure --prefix=/usr/local/nginx     --add-module=../nginx-vod-module     --add-module=../nginx-secure-token-module     --with-http_ssl_module     --with-file-aio     --with-threads     --with-cc-opt="-O3" &&     make && make install &&     rm -rf /nginx /nginx-vod-module /nginx-secure-token-module &&     rm -rf /usr/local/nginx/html /usr/local/nginx/conf/*.default

FROM alpine:3.21 AS sidecar-builder
WORKDIR /sidecar
RUN apk add --no-cache python3 py3-pip && python3 -m venv /venv
RUN /venv/bin/pip install --no-cache-dir fastapi uvicorn requests python-multipart rapidfuzz unidecode
COPY sidecar/main.py .

FROM --platform=linux/arm64 ghcr.io/webtor-io/torrent-store:arm64-local AS torrent-store
FROM --platform=linux/arm64 ghcr.io/webtor-io/magnet2torrent:arm64-local AS magnet2torrent
FROM --platform=linux/arm64 ghcr.io/webtor-io/external-proxy:arm64-local AS external-proxy
FROM --platform=linux/arm64 ghcr.io/webtor-io/torrent-web-seeder:arm64-local AS torrent-web-seeder
FROM --platform=linux/arm64 ghcr.io/webtor-io/torrent-web-seeder-cleaner:arm64-local AS torrent-web-seeder-cleaner
FROM --platform=linux/arm64 golang:1.26-alpine AS build-content-transcoder
ARG CONTENT_TRANSCODER_COMMIT
ENV CGO_ENABLED=0 GOOS=linux
RUN apk add --no-cache git
WORKDIR /src/content-transcoder
RUN git clone https://github.com/webtor-io/content-transcoder . && \
    git checkout ${CONTENT_TRANSCODER_COMMIT} && \
    go build -ldflags '-w -s' -a -installsuffix cgo -o /app/server
FROM --platform=linux/arm64 ghcr.io/webtor-io/torrent-archiver:arm64-local AS torrent-archiver
FROM --platform=linux/arm64 ghcr.io/webtor-io/srt2vtt:arm64-local AS srt2vtt
FROM --platform=linux/arm64 ghcr.io/webtor-io/torrent-http-proxy:arm64-local AS torrent-http-proxy
FROM --platform=linux/arm64 ghcr.io/webtor-io/rest-api:arm64-local AS rest-api
FROM --platform=linux/arm64 golang:1.26-alpine AS web-ui-builder
RUN apk add --no-cache git
WORKDIR /app
COPY web-ui-src/go.mod web-ui-src/go.sum ./

COPY web-ui-src/ ./
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build \
    -ldflags '-w -s -X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=ignore' \
    -a -installsuffix cgo -o server .

FROM --platform=linux/arm64 ghcr.io/webtor-io/web-ui:arm64-local AS web-ui-official
FROM --platform=linux/arm64 ghcr.io/webtor-io/claims-provider:arm64-local AS claims-provider

FROM alpine:$ALPINE_VER AS base
ARG S6_OVERLAY_VER="3.2.0.2"
ARG S6_VERBOSITY=2
ENV S6_VERBOSITY=$S6_VERBOSITY

ADD https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VER}/s6-overlay-noarch.tar.xz /tmp
RUN tar -C / -Jxpf /tmp/s6-overlay-noarch.tar.xz
ADD https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VER}/s6-overlay-aarch64.tar.xz /tmp
RUN tar -C / -Jxpf /tmp/s6-overlay-aarch64.tar.xz

RUN apk --no-cache add redis ffmpeg ca-certificates openssl pcre zlib envsubst uuidgen postgresql postgresql-client postgresql-contrib curl python3 nginx tzdata

WORKDIR /app
RUN mkdir "bin"

COPY --from=sidecar-builder /venv /venv
COPY --from=sidecar-builder /sidecar/main.py /app/sidecar/main.py

COPY --from=torrent-store /server /app/torrent-store
COPY --from=magnet2torrent /server /app/magnet2torrent
COPY --from=external-proxy /server /app/external-proxy
COPY --from=torrent-web-seeder /server /app/torrent-web-seeder
COPY --from=torrent-web-seeder-cleaner /server /app/torrent-web-seeder-cleaner
COPY --from=build-content-transcoder /app/server /app/content-transcoder
COPY --from=torrent-archiver /server /app/torrent-archiver
COPY --from=srt2vtt /server /app/srt2vtt
COPY --from=torrent-http-proxy /server /app/torrent-http-proxy
COPY --from=rest-api /server /app/rest-api
COPY --from=web-ui-builder /app/server /app/web-ui
COPY --from=claims-provider /server /app/claims-provider

    COPY web-ui-src/templates /app/templates
    COPY web-ui-src/pub /app/pub
    COPY web-ui-src/migrations /app/migrations
    COPY web-ui-src/assets/dist /app/assets/dist

COPY --from=build-nginx-vod /usr/local/nginx /usr/local/nginx

COPY etc/webtor /etc/webtor
COPY etc/nginx/conf /usr/local/nginx/conf
COPY services.d /etc/services.d
COPY cont-init.d /etc/cont-init.d

RUN find /etc/services.d -type f -name run -exec chmod +x {} +
RUN find /etc/cont-init.d -type f -exec chmod +x {} +

EXPOSE 8080
EXPOSE 5432
ENTRYPOINT ["/init"]
