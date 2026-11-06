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
ARG VERSION=dev
# The shell strips the tag's "v": Docker's own substitution wouldn't.
RUN v="${VERSION#v}" && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags "-X main.version=$v" -o /pergent ./cmd/

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

# Install opencode: its release names amd64 "x64". The checksums are the
# release assets' digests: change them together with the version.
ARG OPENCODE_VERSION=1.19.4
ARG OPENCODE_SHA256_X64=849bd6b3d959a1d2ef200ed1634a51c79a45ca236aa26665d75debd694805cce
ARG OPENCODE_SHA256_ARM64=8568230ccd847dc93e95b9f6894379077d3e014a16b48040743573e77c23e85f
RUN case "${TARGETARCH}" in \
      amd64) opencode_arch=x64; opencode_sha256="${OPENCODE_SHA256_X64}" ;; \
      arm64) opencode_arch=arm64; opencode_sha256="${OPENCODE_SHA256_ARM64}" ;; \
      *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && curl -fsSL -o /tmp/opencode.tar.gz \
    "https://github.com/anomalyco/opencode/releases/download/v${OPENCODE_VERSION}/opencode-linux-${opencode_arch}.tar.gz" \
    && echo "${opencode_sha256}  /tmp/opencode.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/opencode.tar.gz -C /usr/local/bin/ \
    && rm /tmp/opencode.tar.gz \
    && chmod +x /usr/local/bin/opencode \
    && opencode --version

COPY --from=builder /pergent /usr/local/bin/pergent

WORKDIR /workspace
