# syntax=docker/dockerfile:1
#
# Distroless eraser image: the static binary for the web UI, send, confirm, ...
# `fill` and `mcp` drive a separate Chromium container over the DevTools port
# (ERASER_CHROME_URL); compose uses the stock chromedp/headless-shell image.
# See docs/docker.md.
#
#   docker compose up -d                       # web UI on http://localhost:8080
#   docker compose --profile browser up -d     # plus Chromium
#
# Publish the web UI port to 127.0.0.1 only: it has no login and holds your
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
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/eraser ./cmd/eraser
# The data directory, pre-created so a new named volume inherits its owner
# (distroless has no shell to mkdir or chown with).
RUN install -d -m 0700 -o 65532 -g 65532 /rootfs/home/nonroot/.eraser


FROM gcr.io/distroless/static-debian13:nonroot AS eraser
COPY --from=build /out/eraser /usr/local/bin/eraser
COPY --from=build --chown=65532:65532 /rootfs/home /home
# ~/.eraser holds config.yaml, history.db, screenshots and the downloaded
# broker list: mount a volume there. WORKDIR is the volume so files commands
# write to the current directory (export's eraser-evidence-*.html) outlive a
# `run --rm` container.
#
# The machine ID, which keys the encrypted mail passwords, lives in the volume
# too: a container has no /etc/machine-id and a new hostname on every recreate.
ENV HOME=/home/nonroot \
    ERASER_CONTAINER=1 \
    ERASER_MACHINE_ID_FILE=/home/nonroot/.eraser/machine-id
WORKDIR /home/nonroot/.eraser
VOLUME ["/home/nonroot/.eraser"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/eraser"]
# 0.0.0.0 inside the container only; publish it as 127.0.0.1:8080:8080.
CMD ["serve", "--listen", "0.0.0.0"]
