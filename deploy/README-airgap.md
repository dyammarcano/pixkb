# pixkb Air-Gap Delivery
<!-- rev:002 -->

How to ship and run `pixkb` on a host with **no internet access**. Two kinds of
"air-gap" exist — pick by what the target host can reach:

- **True isolation** (no network out): move bytes over an approved channel
  (`docker save`/`load`, or a copied binary). Options A and B.
- **Corporate network** (no public internet, but an internal container registry
  and the Postgres server are reachable): push to the internal registry and let
  hosts pull. **Option C** — usually the right one for an enterprise LAN.

---

## Option A — Batteries-included image (recommended)

`deploy/Dockerfile` extends the pre-staged `pgvector/pgvector:pg17` base and
bakes in:

- the static `pixkb` binary (`CGO_ENABLED=0`, hashing embedder),
- initdb hooks that `CREATE EXTENSION vector + btree_gist` and run
  `pixkb db up` on first cluster init (`deploy/initdb/`),
- a slot for your OKF bundle at `/kb` (`deploy/kb-data/`).

One image carries Postgres, pgvector, the schema, the CLI, and (optionally) the
knowledge bundle — nothing else is fetched on the air-gap host.

### 1. Build on a connected host

```bash
# Optional: bake your knowledge bundle into the image first.
cp -r kb/* deploy/kb-data/

bash deploy/build-image.sh            # tags pixkb-airgap:latest
```

### 2. Export to a tarball

```bash
docker save -o pixkb-airgap_latest.tar pixkb-airgap:latest
```

### 3. Transfer

Move `pixkb-airgap_latest.tar` to the air-gap host over your approved channel
(USB, data diode, sneakernet).

### 4. Load + run on the air-gap host

```bash
docker load -i pixkb-airgap_latest.tar

docker run -d --name pixkb -p 5432:5432 \
  -e POSTGRES_USER=pixkb -e POSTGRES_PASSWORD=pixkb -e POSTGRES_DB=pixkb \
  -v pixkb_pgdata:/var/lib/postgresql/data \
  pixkb-airgap:latest
```

On first start the initdb hooks enable the extensions and apply the schema
automatically. Then drive the CLI from inside the container:

```bash
docker exec -it pixkb pixkb ingest      # /kb bundle is baked in at PIXKB_BUNDLE
docker exec -it pixkb pixkb search "devolucao refund"
docker exec -it pixkb pixkb doctor       # air-gap readiness checks
```

---

## Option B — Stock pgvector image + separate binary

Use `deploy/docker-compose.yml` (stock `pgvector/pgvector:pg17`) and ship the
`pixkb` binary from a GoReleaser archive alongside it. The DB and the CLI travel
separately.

### 1. Stage the pgvector image online

```bash
docker pull pgvector/pgvector:pg17
docker save pgvector/pgvector:pg17 -o pgvector-pg17.tar
```

### 2. Transfer + load on the air-gap host

```bash
docker load -i pgvector-pg17.tar
```

### 3. Bring up Postgres and run pixkb

```bash
docker compose -f deploy/docker-compose.yml up -d

export PIXKB_DSN='postgres://pixkb:pixkb@localhost:5432/pixkb?sslmode=disable'
# A superuser must enable the extensions once (the schema needs both):
#   CREATE EXTENSION vector; CREATE EXTENSION btree_gist;

pixkb db up        # apply schema
pixkb ingest       # ingest the staged bundle / PDFs / repo mirrors
pixkb search "credit transfer"
```

The OKF bundle (`kb/`), any BCB PDFs, and pre-cloned repo mirrors are staged the
same way — copied to the host ahead of time and referenced from `pixkb.yaml`
(`bundle_dir`, `pdfs`, `mirror_dir` / `repos`). See `pixkb.yaml.example`.

---

## Option C — Internal container registry (corporate network)

Use when the target hosts have **no public internet** but **can reach an internal
registry** (Harbor / Artifactory / Nexus / a mirrored ECR) and the corporate
Postgres. No `docker save`/USB step — hosts pull from the registry.

### 1. Build + push from a host that can reach the registry

```bash
# Public bases reachable here? plain build. Otherwise pre-mirror them and pass
# BUILD_IMAGE / BASE_IMAGE so the build itself never hits the public internet.
REGISTRY=registry.corp.internal/pix bash deploy/push-image.sh v1

# Fully-internal build (bases come from your mirror):
REGISTRY=registry.corp.internal/pix \
  BASE_IMAGE=registry.corp.internal/mirror/pgvector/pgvector:pg17 \
  BUILD_IMAGE=registry.corp.internal/mirror/golang:1.26 \
  bash deploy/push-image.sh v1
```

`deploy/push-image.sh` builds `deploy/Dockerfile` and pushes
`${REGISTRY}/pixkb-airgap:${TAG}`. Run `docker login <registry>` first. The
Dockerfile's `BUILD_IMAGE` / `BASE_IMAGE` ARGs let the whole build stay on the
internal mirror.

### 2. Pull + run on a corp host

```bash
docker pull registry.corp.internal/pix/pixkb-airgap:v1
docker run -d --name pixkb -p 5432:5432 \
  -e POSTGRES_USER=pixkb -e POSTGRES_PASSWORD=pixkb -e POSTGRES_DB=pixkb \
  -v pixkb_pgdata:/var/lib/postgresql/data \
  registry.corp.internal/pix/pixkb-airgap:v1
```

Or reference the image in a k8s `Deployment` / compose `image:` field. If the
corp already runs a managed Postgres (e.g. the one at `192.168.15.100`), you do
not need the batteries-included image at all — ship just the binary (Option B,
skip the pgvector container) and point `PIXKB_DSN` at the managed server.

---

## Notes

- **Image tag is `pg17`**, matching `deploy/docker-compose.yml` and the
  `.goreleaser.yaml` air-gap notes. Keep the three in sync if you bump it.
- **Extensions are not auto-installed by stock pgvector.** The extension is
  compiled into the image, but a superuser still has to `CREATE EXTENSION` once
  (Option A does this for you via the initdb hook).
- **Updating the bundle offline**: re-run `pixkb ingest` after staging new
  artifacts; the Postgres index is rebuildable from the OKF bundle at any time
  with `pixkb reindex` — no rebuild of the image required.
