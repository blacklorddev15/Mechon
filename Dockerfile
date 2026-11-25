# Mechon panel image, sized for Heroku's container stack (and any other PaaS that builds a Dockerfile).
#
#   docker build -t mechon-panel .
#   docker run --rm -p 8080:8080 \
#     -e MECHON_DATABASE_URL=postgres://... -e MECHON_SECRET_KEY=... \
#     -e MECHON_PUBLIC_URL=http://localhost:8080 mechon-panel
#
# The panel is the control plane only. Bots run on nodes (`mechon-agent`) next to Docker, which
# cannot be hosted on Heroku: see docs/heroku.md.

# syntax=docker/dockerfile:1

# ---- 1. Build the React UI, which is embedded into the Go binary ------------------------------
FROM node:22-bookworm-slim AS web
WORKDIR /src/web
RUN corepack enable && corepack prepare pnpm@10 --activate
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# ---- 2. Build the panel ----------------------------------------------------------------------
FROM golang:1.26-bookworm AS build
WORKDIR /src
# Dependencies first, so the module cache survives changes to the source.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=container
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/mechon ./cmd/mechon

# ---- 3. Runtime ------------------------------------------------------------------------------
FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 10001 --home-dir /nonexistent --shell /usr/sbin/nologin mechon \
 && mkdir -p /tmp/mechon-data \
 && chown mechon:mechon /tmp/mechon-data

COPY --from=build /out/mechon /usr/local/bin/mechon

# MECHON_LISTEN is deliberately not set: the panel falls back to 0.0.0.0:$PORT, which is the only
# port a Heroku web dyno may bind. Set MECHON_LISTEN yourself to override it.
ENV MECHON_DATA_DIR=/tmp/mechon-data
EXPOSE 8080

USER mechon
ENTRYPOINT ["/usr/local/bin/mechon"]
CMD ["serve"]
