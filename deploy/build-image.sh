#!/usr/bin/env bash
#
# Build the pixkb air-gap image and print the offline-transfer instructions.
#
# This builds deploy/Dockerfile (pgvector/pgvector:pg17 + the pixkb binary +
# schema initdb hook + baked OKF bundle slot) and tags it pixkb-airgap:latest.
# Run this on a CONNECTED host; transfer the resulting tar to the air-gap host.
#
# Usage:
#   bash deploy/build-image.sh [IMAGE_TAG]
#
# Env:
#   IMAGE_TAG   Override the image tag (default: pixkb-airgap:latest).
#   DOCKER      Container CLI to use (default: docker; e.g. podman).
set -euo pipefail

# Resolve the repo root from this script's location so it runs from anywhere.
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." >/dev/null 2>&1 && pwd)"

DOCKER="${DOCKER:-docker}"
IMAGE_TAG="${IMAGE_TAG:-${1:-pixkb-airgap:latest}}"
TAR_NAME="${IMAGE_TAG//[:\/]/_}.tar"

echo ">> Building ${IMAGE_TAG} from deploy/Dockerfile"
# Build context is the repo root so the multi-stage build can see go.mod, the
# source tree, and deploy/ (initdb hooks + kb-data bundle slot).
"${DOCKER}" build \
  -f "${REPO_ROOT}/deploy/Dockerfile" \
  -t "${IMAGE_TAG}" \
  "${REPO_ROOT}"

echo
echo ">> Built ${IMAGE_TAG}."
echo
echo "Next — stage it for the air-gap host:"
echo
echo "  # 1. On THIS (connected) host, export the image to a tarball:"
echo "  ${DOCKER} save -o ${TAR_NAME} ${IMAGE_TAG}"
echo
echo "  # 2. Transfer ${TAR_NAME} to the air-gap host (USB / approved channel)."
echo
echo "  # 3. On the AIR-GAP host, load it:"
echo "  ${DOCKER} load -i ${TAR_NAME}"
echo
echo "  # 4. Run it (see deploy/README-airgap.md for compose + run details):"
echo "  ${DOCKER} run -d --name pixkb -p 5432:5432 \\"
echo "    -e POSTGRES_USER=pixkb -e POSTGRES_PASSWORD=pixkb -e POSTGRES_DB=pixkb \\"
echo "    -v pixkb_pgdata:/var/lib/postgresql/data ${IMAGE_TAG}"
