# =============================================================================
# Dockerfile — multi-stage build for the online-shop Go binary
# =============================================================================
#
# Stage 1 (builder):  golang:1.23-alpine — has go toolchain + git + ca-certs
# Stage 2 (runtime): gcr.io/distroless/static-debian12:nonroot — no shell,
#                     no package manager, ~2 MB base, runs as UID 65532.
#
# Why the FROM lines reference docker.io / gcr.io directly (NOT our AR
# remotes europe-west2-docker.pkg.dev/.../dockerhub-remote):
#   - The Dockerfile is built INSIDE docker:dind on a GitLab shared runner
#   - That runner has no GCP / AR authentication available at FROM-image-pull
#     time (auth only kicks in inside the build job's `script:`)
#   - Both registries are reachable now that the on-prem broker rules are
#     enabled (the 2026-05-07 unblock); see argocd.tf header comment for
#     the same egress story
#   - Once the image is BUILT, it gets PUSHED to AR sbx-images — the cluster
#     pulls from there (ArgoCD-synced Deployment references the AR path)
#
# CGO_ENABLED=0 + linux/amd64 + -ldflags "-s -w" produce a small static
# binary with no libc dependency, which is exactly what the distroless
# static base image supports.

# -----------------------------------------------------------------------------
# Stage 1 — build the Go binary
# -----------------------------------------------------------------------------
FROM golang:1.23-alpine AS builder

# Add ca-certs (the runtime image is distroless; we'll copy them across)
RUN apk add --no-cache ca-certificates

WORKDIR /src

# Cache module download separately from source compile so a code-only edit
# doesn't force a full module re-download.
COPY go.mod ./
# (go.sum intentionally not COPY'd — std-lib only, no external deps)
RUN go mod download

# Copy everything else the binary needs to compile (Go source + the
# embedded HTML template).
COPY main.go index.html ./

# CGO off → static binary; -ldflags strip symbols + DWARF for size.
# GOOS/GOARCH explicit so the same Dockerfile builds the same artifact
# on any host architecture (CI runner, Apple Silicon dev laptop, etc.).
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -ldflags="-s -w" -o /out/server .

# -----------------------------------------------------------------------------
# Stage 2 — minimal runtime
# -----------------------------------------------------------------------------
# distroless static = scratch + ca-certificates + tzdata + nonroot user.
# No shell, no apk, nothing else. Anything more would just be attack surface.
FROM gcr.io/distroless/static-debian12:nonroot

# Copy the static binary across (only thing we need from the builder stage).
COPY --from=builder /out/server /server

# Document the listen port. k8s probes target this directly.
EXPOSE 8080

# Run as the distroless 'nonroot' user (UID 65532) — already the default,
# being explicit makes it visible at scan time.
USER nonroot:nonroot

# ENTRYPOINT (not CMD) so flags can't be appended by k8s args without
# them looking like server flags.
ENTRYPOINT ["/server"]
