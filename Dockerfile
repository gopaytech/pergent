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

# The pergent-security image (`--target security`): `pergent scan` with node
# for the skill's validators, and no curl or wget. The last stage below stays
# the review image, so a build with no --target is unchanged.

# opencode, downloaded on the build machine. Same pin and checksums as the
# review image below: change them together.
FROM --platform=$BUILDPLATFORM debian:trixie-slim AS opencode

ARG TARGETARCH
ARG OPENCODE_VERSION=2.3.2
ARG OPENCODE_SHA256_X64=563b5d663637226b6e3bb4bb6803281bc39b0d2cd88db06d674b6420aab009b0
ARG OPENCODE_SHA256_ARM64=9d10185acdb72336e086e699ca363a6065f410fd3c1caed0dc8046e95ac06ccb
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
RUN case "${TARGETARCH}" in \
      amd64) opencode_arch=x64; opencode_sha256="${OPENCODE_SHA256_X64}" ;; \
      arm64) opencode_arch=arm64; opencode_sha256="${OPENCODE_SHA256_ARM64}" ;; \
      *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && curl -fsSL -o /tmp/opencode.tar.gz \
    "https://github.com/anomalyco/opencode/releases/download/v${OPENCODE_VERSION}/opencode-linux-${opencode_arch}.tar.gz" \
    && echo "${opencode_sha256}  /tmp/opencode.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/opencode.tar.gz -C / opencode \
    && chmod +x /opencode

# The security-audit skill at one commit. It must equal the manifest's
# `commit` (a test and the grep below check it). The folder holds the skill's
# files, its LICENSE and our manifest.
FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS skills

ARG SECURITY_AUDIT_COMMIT=c1c8a8c1471069fb0e188eeaff69b8e8db6564a8
RUN git init /src \
    && git -C /src fetch --depth 1 https://github.com/cloudflare/security-audit-skill.git "${SECURITY_AUDIT_COMMIT}" \
    && git -C /src checkout FETCH_HEAD \
    && test "$(git -C /src rev-parse HEAD)" = "${SECURITY_AUDIT_COMMIT}"
# The skill's licence must ship with it: a missing LICENSE fails the build here.
RUN mkdir -p /out/security-audit \
    && cp -a /src/skills/security-audit/. /out/security-audit/ \
    && test -f /src/LICENSE \
    && cp /src/LICENSE /out/security-audit/LICENSE
COPY security-skills/security-audit/pergent-scan.yaml /out/security-audit/pergent-scan.yaml
RUN grep -qx "commit: ${SECURITY_AUDIT_COMMIT}" /out/security-audit/pergent-scan.yaml

FROM debian:trixie-slim AS security

# node runs the skill's validators. git and ripgrep are for the agent's tools.
# No curl or wget: the audited code and the model must not fetch anything.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    git \
    ripgrep \
    nodejs \
    && rm -rf /var/lib/apt/lists/*

COPY --from=opencode /opencode /usr/local/bin/opencode
COPY --from=builder /pergent /usr/local/bin/pergent
COPY --from=skills --chown=root:root /out/ /opt/pergent/security-skills/

# pergent reads the skill's report-schema.json after the model had a shell, so
# the scan's user must not be able to write in the skill folder.
RUN chmod -R go-w /opt/pergent/security-skills \
    && test -z "$(find /opt/pergent/security-skills -perm /022)" \
    && test -z "$(find /opt/pergent/security-skills ! -user root)"
RUN opencode --version \
    && node --version \
    && pergent scan --check-skill /opt/pergent/security-skills/security-audit \
    && ! command -v curl \
    && ! command -v wget

WORKDIR /workspace

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
# release assets' digests: change them together with the version. The archive
# holds one top-level `opencode`; naming it makes a changed layout fail here.
ARG OPENCODE_VERSION=2.3.2
ARG OPENCODE_SHA256_X64=563b5d663637226b6e3bb4bb6803281bc39b0d2cd88db06d674b6420aab009b0
ARG OPENCODE_SHA256_ARM64=9d10185acdb72336e086e699ca363a6065f410fd3c1caed0dc8046e95ac06ccb
RUN case "${TARGETARCH}" in \
      amd64) opencode_arch=x64; opencode_sha256="${OPENCODE_SHA256_X64}" ;; \
      arm64) opencode_arch=arm64; opencode_sha256="${OPENCODE_SHA256_ARM64}" ;; \
      *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && curl -fsSL -o /tmp/opencode.tar.gz \
    "https://github.com/anomalyco/opencode/releases/download/v${OPENCODE_VERSION}/opencode-linux-${opencode_arch}.tar.gz" \
    && echo "${opencode_sha256}  /tmp/opencode.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/opencode.tar.gz -C /usr/local/bin/ opencode \
    && rm /tmp/opencode.tar.gz \
    && chmod +x /usr/local/bin/opencode \
    && opencode --version

COPY --from=builder /pergent /usr/local/bin/pergent

WORKDIR /workspace
