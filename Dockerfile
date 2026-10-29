# syntax=docker/dockerfile:1
#
# Single image: the Next.js UI is exported to static files and embedded in the
# Go binary. Runtime image is distroless (~25 MB), ideal for a Raspberry Pi.
#
# Build on the Pi:            docker compose build
# Cross-build from a laptop:  docker buildx build --platform linux/arm64 -t talos-controller --load .

# ---------- UI ----------
FROM --platform=$BUILDPLATFORM node:22-alpine AS ui
WORKDIR /ui
COPY frontend/package.json frontend/package-lock.json* ./
RUN --mount=type=cache,target=/root/.npm \
    if [ -f package-lock.json ]; then npm ci --no-audit --no-fund; else npm install --no-audit --no-fund; fi
COPY frontend/ ./
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

# ---------- Go ----------
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ENV GOTOOLCHAIN=auto CGO_ENABLED=0
WORKDIR /src
COPY backend/ ./
# go.sum is generated on first build if it is missing.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod tidy && go mod download
COPY --from=ui /ui/out ./web/dist
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/talos-controller . \
    && mkdir -p /out/data

# ---------- runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/talos-controller /talos-controller
COPY --from=build --chown=65532:65532 /out/data /data
ENV LISTEN_ADDR=:8080 \
    TALOSCONFIG=/config/talosconfig \
    KUBECONFIG=/config/kubeconfig \
    DATA_DIR=/data
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/talos-controller", "healthcheck"]
ENTRYPOINT ["/talos-controller"]
