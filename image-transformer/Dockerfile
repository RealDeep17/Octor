FROM alpine:latest as libwebp

RUN apk add --no-cache --update libpng-dev libjpeg-turbo-dev giflib-dev tiff-dev autoconf automake make gcc g++ wget && \
    wget https://storage.googleapis.com/downloads.webmproject.org/releases/webp/libwebp-0.6.0.tar.gz && \
    tar -xvzf libwebp-0.6.0.tar.gz && \
    mv libwebp-0.6.0 libwebp && \
    rm libwebp-0.6.0.tar.gz && \
    cd /libwebp && \
    ./configure && \
    make && \
    make install

FROM golang:latest as build

# set work dir
WORKDIR /app

# copy the source files
COPY . .

# enable modules
ENV GO111MODULE=on

# disable crosscompiling
ENV CGO_ENABLED=0

# compile linux only
ENV GOOS=linux

# build the binary with debug information removed
RUN go build -mod=vendor -ldflags '-w -s' -a -installsuffix cgo -o server

FROM alpine:latest

# install webp deps
RUN apk add --no-cache --update libpng-dev libjpeg-turbo-dev giflib-dev tiff-dev

# copy our static linked library
COPY --from=build /app/server .

# copy libwebp
COPY --from=libwebp /usr/local/bin/dwebp ./vendor/webp/dwebp
COPY --from=libwebp /usr/local/bin/cwebp ./vendor/webp/cwebp
COPY --from=libwebp /usr/local/lib/libweb* /usr/local/lib/

# tell we are exposing our service on port 8080, 8081
EXPOSE 8080 8081

# run it!
CMD ["./server"]
