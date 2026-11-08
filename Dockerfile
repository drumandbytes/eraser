# syntax=docker/dockerfile:1
#
# Eraser in a container, with headless Chromium for `fill` and the `mcp`
# browser tools. See docs/docker.md.
#
#   docker compose up -d        # web UI on http://localhost:8080
#
# Publish the port to 127.0.0.1 only: the web UI has no login and holds your
# mail credentials.

ARG GO_VERSION=1.26
ARG DEBIAN_VERSION=trixie

FROM golang:${GO_VERSION}-${DEBIAN_VERSION} AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ARG VERSION=dev
# Same flags as .goreleaser.yaml: pure Go (modernc SQLite), no cgo.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/eraser ./cmd/eraser

FROM debian:${DEBIAN_VERSION}-slim

# chromium: chromedp finds /usr/bin/chromium on PATH by itself.
# fonts: broker forms render (and screenshot) with real glyphs.
# tini: reaps Chromium's child processes when eraser is PID 1.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates \
      chromium \
      fonts-liberation \
      fonts-noto-color-emoji \
      tini \
      tzdata \
 && rm -rf /var/lib/apt/lists/*

# Unprivileged user. ~/.eraser holds config.yaml, history.db, screenshots and
# the downloaded broker list: mount a volume there.
# /etc/machine-id points into the volume, where docker/entrypoint.sh creates
# it once: the key that encrypts your mail passwords must survive container
# recreation and image upgrades.
RUN useradd --create-home --uid 1000 --shell /usr/sbin/nologin eraser \
 && install -d -m 0700 -o eraser -g eraser /home/eraser/.eraser \
 && rm -f /etc/machine-id \
 && ln -s /home/eraser/.eraser/machine-id /etc/machine-id

COPY --from=build /out/eraser /usr/local/bin/eraser
COPY --chmod=0755 docker/entrypoint.sh /usr/local/bin/eraser-entrypoint

USER eraser
WORKDIR /home/eraser
ENV HOME=/home/eraser \
    ERASER_CONTAINER=1

VOLUME ["/home/eraser/.eraser"]
EXPOSE 8080

ENTRYPOINT ["/usr/bin/tini", "--", "eraser-entrypoint"]
# 0.0.0.0 inside the container only; publish it as 127.0.0.1:8080:8080.
CMD ["serve", "--listen", "0.0.0.0"]
