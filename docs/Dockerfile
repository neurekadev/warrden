# syntax=docker/dockerfile:1.7

FROM node:24-bookworm-slim AS build

ARG GIT_TAG=dev
ARG GIT_HASH=unknown

WORKDIR /app/src

COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci

COPY . .
RUN npm run build

FROM ubuntu:24.04

ARG GIT_TAG=dev
ARG GIT_HASH=unknown

LABEL org.opencontainers.image.title="wArrden Docs" \
      org.opencontainers.image.version=$GIT_TAG \
      org.opencontainers.image.revision=$GIT_HASH

ENV GIT_TAG=$GIT_TAG \
    GIT_HASH=$GIT_HASH \
    PATH=/app/bin:$PATH

RUN apt-get update && \
    apt-get install -y --no-install-recommends nginx && \
    rm -rf /var/lib/apt/lists/* && \
    mkdir -p /app/bin /app/config /app/data/site /app/cache/nginx && \
    ln -s /usr/sbin/nginx /app/bin/nginx

WORKDIR /app

COPY nginx.conf /app/config/nginx.conf
COPY --from=build /app/src/out/ /app/data/site/

EXPOSE 3000

ENTRYPOINT ["/app/bin/nginx", "-c", "/app/config/nginx.conf", "-g", "daemon off;"]
