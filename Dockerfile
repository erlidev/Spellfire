# syntax=docker/dockerfile:1

# --- Stage 1: build the Vite/TypeScript client into dist/ ---
FROM node:20-alpine AS client
WORKDIR /app
# Install deps against the lockfile first so this layer caches across source edits.
COPY package.json package-lock.json ./
RUN npm ci
COPY tsconfig.json tsconfig.build.json ./
COPY data ./data
COPY web ./web
RUN npm run build

# --- Stage 2: build the Go server binary (pure-Go SQLite, so CGO is off) ---
FROM golang:1.22-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY server ./server
COPY proto ./proto
# The same tuning tables the client stage bundled; the server embeds them.
COPY data ./data
# Stamp the build time (from the build host's clock) and, if provided, the git
# commit; the .git dir is not in the build context, so the commit comes via arg.
ARG BUILD_COMMIT=""
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w \
      -X spellfire/server/internal/build.Time=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
      -X spellfire/server/internal/build.Commit=${BUILD_COMMIT}" \
    -o /out/spellfire-server ./server/cmd/spellfire

# --- Stage 3: minimal runtime image ---
FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates wget && \
    adduser -D -H -u 10001 spellfire
WORKDIR /app
COPY --from=server /out/spellfire-server ./spellfire-server
COPY --from=client /app/dist ./web

# Static assets live in ./web; the SQLite DB lives on a mounted volume.
ENV SPELLFIRE_ADDRESS=":8080" \
    SPELLFIRE_WEB_ROOT="/app/web" \
    SPELLFIRE_DATABASE="/data/spellfire.db"

RUN mkdir -p /data && chown spellfire:spellfire /data
VOLUME ["/data"]
USER spellfire
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/api/health || exit 1

ENTRYPOINT ["/app/spellfire-server"]
