# syntax=docker/dockerfile:1
#
# Air-gap "batteries-included" image for pixkb.
#
# It extends the pre-staged pgvector/pgvector:pg17 image (Postgres + pgvector
# already compiled in) and bakes the pixkb binary, the schema migration as an
# initdb hook, and a slot for the OKF knowledge bundle directly into the image.
# The result is a single tarball you can `docker save` on a connected host and
# `docker load` on an isolated one — no on-host build, no network pulls.
#
# Build (online host):  bash deploy/build-image.sh
# See deploy/README-airgap.md for the full offline transfer workflow.

# Base images are ARGs so a corporate build can point them at an INTERNAL
# mirror/registry (no public pulls). Defaults are the public upstreams.
#   --build-arg BUILD_IMAGE=registry.corp.internal/mirror/golang:1.26
#   --build-arg BASE_IMAGE=registry.corp.internal/mirror/pgvector/pgvector:pg17
ARG BUILD_IMAGE=golang:1.26
ARG BASE_IMAGE=pgvector/pgvector:pg17

# ---- Stage 1: build the pure-Go static pixkb binary -------------------------
FROM ${BUILD_IMAGE} AS build

WORKDIR /src

# Prime the module cache with just the manifests for better layer caching.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 => fully static, portable binary (hashing embedder, no onnx).
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/pixkb ./cmd/pixkb

# ---- Stage 2: pgvector base + pixkb + initdb hook + bundle slot -------------
# Re-declare ARG after FROM resets the build-arg scope (Docker requirement).
ARG BASE_IMAGE=pgvector/pgvector:pg17
FROM ${BASE_IMAGE}

# pixkb CLI on PATH inside the DB container (run `pixkb ingest`, `pixkb search`).
COPY --from=build /out/pixkb /usr/local/bin/pixkb

# initdb hooks: run once, in lexical order, on first cluster init against the
# local Postgres. 10-extensions.sql enables vector + btree_gist; 20-pixkb-db-up
# applies the pixkb schema via `pixkb db up`.
COPY deploy/initdb/ /docker-entrypoint-initdb.d/
# Normalize line endings (the repo may be checked out with CRLF on Windows; a
# CRLF shebang yields exit 127 "bad interpreter") and make the hooks executable.
RUN sed -i 's/\r$//' /docker-entrypoint-initdb.d/* \
 && chmod +x /docker-entrypoint-initdb.d/*.sh

# OKF bundle slot. Stage your kb-data bundle into deploy/kb-data/ before
# building to bake it in (the .gitkeep keeps an empty slot valid otherwise).
# At runtime: pixkb reads it via PIXKB_BUNDLE (default /kb).
COPY deploy/kb-data/ /kb/
ENV PIXKB_BUNDLE=/kb

# DSN pointing at the co-located Postgres so `pixkb` works inside the container.
ENV PIXKB_DSN=postgres://pixkb:pixkb@localhost:5432/pixkb?sslmode=disable
