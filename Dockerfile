# Multi-arch: linux/amd64 and linux/arm64. The Go stage runs on the build
# machine and cross-compiles; the final stage runs on the target platform
# (QEMU when it differs from the build machine).
FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -o /pergent ./cmd/

FROM debian:trixie-slim

ARG TARGETARCH

# ripgrep: opencode's glob and grep tools use rg from PATH. Without it opencode
# downloads one at run time, which fails where the container can't chown
# (capabilities dropped) and on clusters with no internet access.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    git \
    curl \
    ripgrep \
    && rm -rf /var/lib/apt/lists/*

# Install opencode: its release names amd64 "x64".
ARG OPENCODE_VERSION=1.19.4
RUN case "${TARGETARCH}" in \
      amd64) opencode_arch=x64 ;; \
      arm64) opencode_arch=arm64 ;; \
      *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && curl -fsSL -o /tmp/opencode.tar.gz \
    "https://github.com/anomalyco/opencode/releases/download/v${OPENCODE_VERSION}/opencode-linux-${opencode_arch}.tar.gz" \
    && tar -xzf /tmp/opencode.tar.gz -C /usr/local/bin/ \
    && rm /tmp/opencode.tar.gz \
    && chmod +x /usr/local/bin/opencode \
    && opencode --version

COPY --from=builder /pergent /usr/local/bin/pergent

WORKDIR /workspace
