# syntax=docker/dockerfile:1

FROM node:24.12.0-slim AS frontend
WORKDIR /frontend-build
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
RUN npm i -g pnpm@11.10.0 && pnpm i --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

FROM golang:1.26.5-alpine3.23 AS manager-build
ARG VERSION=local
ARG GIT_COMMIT=unknown
# LAELIA_BUILD_PROXY is a custom arg so the proxy only reaches stages that
# declare it; BuildKit would otherwise auto-inject standard proxy args into
# every stage, including the final runtime images.
ARG LAELIA_BUILD_PROXY
ENV http_proxy=${LAELIA_BUILD_PROXY} https_proxy=${LAELIA_BUILD_PROXY} no_proxy=localhost,127.0.0.1
ENV HTTP_PROXY=${LAELIA_BUILD_PROXY} HTTPS_PROXY=${LAELIA_BUILD_PROXY} NO_PROXY=localhost,127.0.0.1
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /frontend-build/dist ./backend/manager/server/dist
RUN CGO_ENABLED=0 go build -tags embed_frontend -ldflags "-w -s" -p=16 \
	-o /out/laelia ./backend/manager/bin/server/main.go

FROM golang:1.26.5-alpine3.23 AS machine-build
ARG VERSION=local
ARG GIT_COMMIT=unknown
ARG TARGETOS
ARG TARGETARCH
# Optional per-step override for the pi download only; when empty,
# build-pi.sh falls back to LAELIA_BUILD_PROXY.
ARG PI_PROXY
ARG LAELIA_BUILD_PROXY
ENV http_proxy=${LAELIA_BUILD_PROXY} https_proxy=${LAELIA_BUILD_PROXY} no_proxy=localhost,127.0.0.1
ENV HTTP_PROXY=${LAELIA_BUILD_PROXY} HTTPS_PROXY=${LAELIA_BUILD_PROXY} NO_PROXY=localhost,127.0.0.1
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN apk add --no-cache curl bash
# build-pi.sh downloads the pi distribution matching the build target platform
# and writes it to backend/agent/pi/embedded/dist, which the release-tagged
# machine build then embeds.
ENV GOOS=${TARGETOS} GOARCH=${TARGETARCH}
ENV PI_PROXY=${PI_PROXY}
RUN ./scripts/build-pi.sh
RUN CGO_ENABLED=0 go build -tags release -ldflags "-w -s" -p=16 \
	-o /out/laelia-machine ./backend/agent/bin/agent/main.go

FROM alpine:3.23 AS manager
ARG VERSION=local
ARG GIT_COMMIT=unknown
LABEL org.opencontainers.image.version=${VERSION}
LABEL org.opencontainers.image.revision=${GIT_COMMIT}
RUN apk add --no-cache ca-certificates curl \
	&& adduser -D -u 1000 laelia
COPY --from=manager-build /out/laelia /usr/local/bin/laelia
USER laelia
EXPOSE 8181
ENV LAELIA_PG_URL=
ENTRYPOINT ["laelia"]
CMD ["--port", "8181"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
	CMD curl -fsS http://localhost:8181/healthz || exit 1

FROM node:24.12.0-slim AS machine
ARG VERSION=local
ARG GIT_COMMIT=unknown
ARG APT_MIRROR
# Codex CLI version spec for the codex ACP v2 provider (npm package). Pin a
# version for reproducible rebuilds; override with CODEX_NPM_SPEC.
ARG CODEX_NPM_SPEC=@openai/codex@0.146.0
LABEL org.opencontainers.image.version=${VERSION}
LABEL org.opencontainers.image.revision=${GIT_COMMIT}
# Agent runtime: pi is a glibc-linked standalone binary (debian base), and the
# agents' shell tool needs a real dev environment: python3, node/npm (already
# in the base image, as is bash), compilers/make, and common CLI utilities.
# Do NOT apt-install bash: node slim images strip man pages and Debian's bash
# postinst fails on the missing bash-builtins.7.gz alternative.
# APT_MIRROR (e.g. http://mirrors.aliyun.com/debian) swaps the Debian repo for
# a faster local mirror; it never leaks proxy settings into the image.
RUN if [ -n "${APT_MIRROR}" ]; then \
		sed -i "s|https\?://deb.debian.org/debian|${APT_MIRROR}|g" \
			/etc/apt/sources.list.d/debian.sources /etc/apt/sources.list 2>/dev/null || true; \
	fi \
	&& apt-get update \
	&& apt-get install -y --no-install-recommends \
		ca-certificates curl git jq wget unzip zip xz-utils ripgrep \
		python3 python3-pip python3-venv python-is-python3 build-essential \
	&& rm -rf /var/lib/apt/lists/* \
	&& useradd --create-home --uid 1001 laelia
# Codex ACP v2 provider: the codex CLI ships an app-server speaking the thread
# protocol. Login/config state is NEVER baked in — operators mount a writable
# CODEX_HOME volume (config.toml + auth/models) at runtime, see
# machine-entrypoint.sh's LAELIA_CODEX_HOME.
RUN npm install -g --no-audit --no-fund "${CODEX_NPM_SPEC}" \
	&& rm -rf /root/.npm
COPY --from=machine-build /out/laelia-machine /usr/local/bin/laelia-machine
COPY scripts/docker/machine-entrypoint.sh /usr/local/bin/machine-entrypoint
USER laelia
ENV HOME=/home/laelia
ENTRYPOINT ["machine-entrypoint"]
