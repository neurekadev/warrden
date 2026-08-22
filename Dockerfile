FROM --platform=$BUILDPLATFORM golang:1.27-bookworm AS build

ARG TARGETOS
ARG TARGETARCH
ARG GIT_TAG=dev
ARG GIT_HASH=unknown

WORKDIR /app/src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

COPY cmd ./cmd
COPY internal ./internal

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /rootfs/app/bin /rootfs/app/data /rootfs/config /rootfs/etc/ssl/certs /rootfs/tmp && \
    chmod 1777 /rootfs/tmp && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false -ldflags="-s -w" -o /rootfs/app/bin/warrden ./cmd/warrden && \
    ln -s warrden /rootfs/app/bin/clear-missing && \
    ln -s warrden /rootfs/app/bin/clear-upgrades && \
    cp /etc/ssl/certs/ca-certificates.crt /rootfs/etc/ssl/certs/ca-certificates.crt

FROM scratch

ARG GIT_TAG=dev
ARG GIT_HASH=unknown

LABEL org.opencontainers.image.title="wArrden" \
      org.opencontainers.image.version=$GIT_TAG \
      org.opencontainers.image.revision=$GIT_HASH

ENV GIT_TAG=$GIT_TAG \
    GIT_HASH=$GIT_HASH \
    PUID=1000 \
    PGID=1000 \
    PATH=/app/bin

WORKDIR /app

COPY --from=build /rootfs /

VOLUME ["/app/data"]

ENTRYPOINT ["/app/bin/warrden"]
