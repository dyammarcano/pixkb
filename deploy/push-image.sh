#!/usr/bin/env bash
#
# Build the pixkb image and push it to an INTERNAL container registry.
#
# For a corporate "air-gap" network: hosts cannot reach the public internet but
# CAN reach an internal registry (Harbor / Artifactory / Nexus / ECR-mirror).
# Build where the public base image is reachable (or pre-mirror the base), then
# push to the internal registry so corp hosts pull from there.
#
# Usage:
#   REGISTRY=registry.corp.internal/pix bash deploy/push-image.sh [TAG]
#
# Env:
#   REGISTRY   Internal registry path WITHOUT trailing slash (required),
#              e.g. registry.corp.internal/pix  or  10.0.0.5:5000/pix
#   TAG        Image tag (default: latest). Also positional $1.
#   IMAGE      Image name (default: pixkb-airgap).
#   BASE_IMAGE Override the FROM base so it points at your internal mirror,
#              e.g. registry.corp.internal/mirror/pgvector/pgvector:pg17
#   DOCKER     Container CLI (default: docker; e.g. podman).
#   PUSH       Set to 0 to build+tag only, skip the push (default: 1).
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." >/dev/null 2>&1 && pwd)"

DOCKER="${DOCKER:-docker}"
IMAGE="${IMAGE:-pixkb-airgap}"
TAG="${TAG:-${1:-latest}}"
PUSH="${PUSH:-1}"

if [[ -z "${REGISTRY:-}" ]]; then
  echo "ERROR: set REGISTRY to your internal registry path, e.g." >&2
  echo "  REGISTRY=registry.corp.internal/pix bash deploy/push-image.sh" >&2
  exit 1
fi

REF="${REGISTRY}/${IMAGE}:${TAG}"

# Allow re-pointing the base image at an internal mirror so the build itself
# never reaches the public internet. Requires a matching ARG in the Dockerfile.
BASE_ARG=()
if [[ -n "${BASE_IMAGE:-}" ]]; then
  BASE_ARG=(--build-arg "BASE_IMAGE=${BASE_IMAGE}")
  echo ">> Using internal base image: ${BASE_IMAGE}"
fi

echo ">> Building ${REF} from deploy/Dockerfile"
"${DOCKER}" build \
  "${BASE_ARG[@]}" \
  -f "${REPO_ROOT}/deploy/Dockerfile" \
  -t "${REF}" \
  "${REPO_ROOT}"

if [[ "${PUSH}" != "1" ]]; then
  echo ">> Built ${REF} (PUSH=0, not pushing)."
  exit 0
fi

echo ">> Pushing ${REF}"
# Assumes the caller already ran: ${DOCKER} login ${REGISTRY%%/*}
"${DOCKER}" push "${REF}"

echo
echo ">> Pushed ${REF}."
echo
echo "On a corp host (no public internet, internal registry reachable):"
echo "  ${DOCKER} pull ${REF}"
echo "  ${DOCKER} run -d --name pixkb -p 5432:5432 \\"
echo "    -e POSTGRES_USER=pixkb -e POSTGRES_PASSWORD=pixkb -e POSTGRES_DB=pixkb \\"
echo "    -v pixkb_pgdata:/var/lib/postgresql/data ${REF}"
echo
echo "Or reference ${REF} in a k8s Deployment / docker-compose image: field."
