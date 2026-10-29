<p align="center"><img src="docs/assets/logo.svg" width="96" alt="Talos Controller logo"></p>

# Talos Controller

A small self-hosted web app for supervising and maintaining a [Talos Linux](https://www.talos.dev) cluster.
Go backend, Next.js frontend, shipped as **one ~25 MB Docker image** that runs comfortably on a Raspberry Pi (arm64).

**Documentation:** see [docs/](docs/README.md) for installation and how to use every page.

## Features

| Area | What you get |
|---|---|
| **Dashboard** | Cluster status, node readiness, Talos/Kubernetes versions, etcd quorum, pod health and 8 health checks (Talos API reachability, node Ready, version skew, etcd, Talos services, memory pressure, disk usage, workloads) |
| **Nodes** | CPU load, memory, uptime per node. Detail page with Talos services (restart), service logs, kernel log (dmesg), filesystem usage, installer image and redacted machine config. Reboot with type-to-confirm |
| **Workloads** | Crash-looping / pending / failed / not-ready pods, deployments/statefulsets/daemonsets missing replicas, warning events from the last 2 h |
| **etcd** | Members, leader, DB size, fault tolerance, alarms |
| **Upgrades** | Latest Talos and Kubernetes releases from GitHub, a *recommended next* version (one minor at a time), and a **rolling Talos upgrade**: control planes first, one node at a time, preflight checks, waits for reboot + correct version + node Ready + etcd healthy before moving on. Each node keeps its own installer image (Image Factory schematic / extensions), only the tag changes |
| **Config patches** | Strategic-merge or JSON6902 patches against a whole role (all control planes / all workers / all nodes) or picked nodes. **Dry run with diff** is required, then a **rolling apply**: dry run on every target first (nothing is applied if one fails), then one node at a time, control planes first, waiting for reboot, node Ready and etcd health before the next |
| **Config drift** | Compares the live, secret-redacted machine configs of nodes with the same role and highlights settings that differ from the majority. Per-node settings (hostname, addresses, install disk…) are hidden by default. One click turns an outlier into a prefilled patch for just that node |
| **Jobs** | Background jobs (upgrades, reboots) with live logs, cancel, and persisted history |
| **Settings** | Upload/paste the talosconfig, pick a context, override endpoints and nodes, optional kubeconfig, test the connection. The app starts without any config and sends you here first |

Kubernetes upgrades are shown as a ready-to-run `talosctl upgrade-k8s` command (with `--dry-run`) rather than executed from the UI. That flow touches every control plane component and bootstrap manifest, and `talosctl` handles it best.

## Architecture

```
browser ──► :8080  Go binary (talos-controller)
                    ├─ /           static Next.js export (embedded)
                    ├─ /api/*      JSON API, cookie session auth
                    ├─ Talos gRPC  (siderolabs/talos/pkg/machinery client)  ─► nodes :50000
                    └─ Kubernetes  (client-go)                              ─► API server :6443
```

```
backend/
  main.go                  entrypoint, embeds web/dist
  internal/auth            single-admin login, HMAC-signed session cookie, lockout
  internal/cluster         Talos + Kubernetes logic: nodes, services, logs, etcd,
                           health checks, workloads, config patching, rolling upgrades
  internal/jobs            background jobs + log history (DATA_DIR/jobs.json)
  internal/releases        GitHub release checks (cached 1 h)
  internal/api             HTTP routes
frontend/
  app/                     pages: dashboard, nodes, node, workloads, etcd, upgrades, config, jobs, login
  components/, lib/        UI primitives, API client + types, polling hook
```

## Run it on the Raspberry Pi

Requires a 64-bit OS (arm64) with Docker + the compose plugin.

```bash
git clone <this repo> talos-controller && cd talos-controller
mkdir -p data
cp .env.example .env
sed -i "s/^SESSION_SECRET=.*/SESSION_SECRET=$(openssl rand -hex 32)/" .env
nano .env            # set ADMIN_PASSWORD (or ADMIN_PASSWORD_BCRYPT), PUID/PGID = `id -u` / `id -g`

docker compose up -d --build
```

Open `http://<pi-ip>:8080`, log in and go to **Settings**: paste or upload your talosconfig, choose the context and set the endpoints (control plane IPs or VIP). Everything is stored in `./data` (mode 600). Alternatively mount a file by setting `TALOSCONFIG_FILE` / `KUBECONFIG_FILE` in `.env`; anything uploaded in the UI takes precedence.

The first build on a Pi 4 takes a while (npm install + Next build + Go compile). To build faster on your laptop and copy the image across:

```bash
docker buildx build --platform linux/arm64 -t talos-controller:latest --load .
docker save talos-controller:latest | ssh pi docker load
```

### Talos credentials

- Upgrades, reboots, config patches and fetching the kubeconfig need an **`os:admin`** talosconfig.
- For a dashboard-only deployment, use a read-only one: `talosctl config new --roles os:reader config/talosconfig`. Actions will then fail with a permission error.
- The `endpoints` in the talosconfig must be reachable from the Pi (control plane IPs or a VIP). Nodes are addressed by their Kubernetes `InternalIP`, falling back to the `nodes` list in the talosconfig.

### Environment variables

| Variable | Default | |
|---|---|---|
| `ADMIN_USER` | `admin` | |
| `ADMIN_PASSWORD` / `ADMIN_PASSWORD_BCRYPT` | – | one is required; bcrypt preferred |
| `SESSION_SECRET` | random | set it so sessions survive restarts |
| `SESSION_TTL` | `12h` | |
| `COOKIE_SECURE` | `false` | `true` when behind HTTPS |
| `TALOSCONFIG` | `/config/talosconfig` | optional, the UI upload takes precedence |
| `TALOS_CONTEXT` | current context | |
| `KUBECONFIG` | `/config/kubeconfig` | optional, the UI upload takes precedence |
| `DATA_DIR` | `/data` | settings, uploaded configs, job history |
| `GITHUB_TOKEN` | – | optional, raises the GitHub API rate limit |
| `LISTEN_ADDR` | `:8080` | |
| `DEBUG` | – | verbose logging |

## CI/CD (GitHub Actions → Docker Hub)

`.github/workflows/ci.yml` runs on every push and pull request:

1. **Backend:** gofmt check, `go vet`, `go test -race`
2. **Frontend:** `npm test` (helper unit tests) and `npm run build` (type check + static export)
3. **Release (push to `main` only):** `scripts/next-version.sh` picks the next version from [Conventional Commits](https://www.conventionalcommits.org) since the last tag (`fix:` → patch, `feat:` → minor, `feat!:` / `BREAKING CHANGE:` → major, minor while on 0.x; anything else → patch; first release `v0.1.0`). It creates the tag and a GitHub release with generated notes. Put `[skip release]` in the commit message to skip it.
4. **Docker:** multi-arch image (`linux/arm64` + `linux/amd64`) after tests pass. Pull requests build without pushing. On `main`: `X.Y.Z`, `X.Y`, `latest` (the newest release), `main` (newest commit) and `sha-xxxxxxx`. The version is shown in the UI sidebar and at `/api/version`.

One-time setup in the GitHub repo (Settings → Secrets and variables → Actions):

| Kind | Name | Value |
|---|---|---|
| Variable | `DOCKERHUB_USERNAME` | your Docker Hub user name |
| Secret | `DOCKERHUB_TOKEN` | a Docker Hub access token with Read & Write (hub.docker.com → Account settings → Personal access tokens) |

```bash
gh variable set DOCKERHUB_USERNAME --body "<your-dockerhub-user>"
gh secret set DOCKERHUB_TOKEN          # paste the token when prompted
```

On the Pi, run the published image instead of building it:

```bash
echo "IMAGE=<your-dockerhub-user>/talos-controller:latest" >> .env
docker compose pull && docker compose up -d --no-build
```

Pin the Pi to a minor version (`IMAGE=<user>/talos-controller:0.2`) to get patch releases automatically, or to `latest` for every release.

## Security notes

This UI holds admin credentials for your cluster. Keep it on your LAN or behind a VPN / reverse proxy with TLS (then set `COOKIE_SECURE=true`). Login is rate-limited (5 failures per 5 min per IP), the session cookie is `HttpOnly` + `SameSite=Strict`, and mutating API calls require a JSON content type. Machine configs are shown with secrets redacted. Destructive actions (reboot, upgrade) need typed confirmation.

## Run locally (Mac)

```bash
./start-local.sh          # Docker (needs Docker Desktop)        -> http://localhost:8080
./start-local.sh dev      # Go + Next.js dev server, hot reload   -> http://localhost:3000
./start-local.sh single   # build UI, run Go binary, no Docker    -> http://localhost:8080
./start-local.sh stop     # stop the Docker container
```

A talosconfig file is optional (you can upload it in Settings). To use one anyway, point at it with `-t/--talosconfig`, `-k/--kubeconfig` (or `--no-kubeconfig`) and `-c/--context`, e.g. `./start-local.sh dev -t ~/clusters/home/talosconfig`. The choice is saved in `.env` and reused on the next run and by `docker compose`. Without flags it looks for `./config/talosconfig`, then `~/.talos/config` (same for kubeconfig). The first run creates `.env` with a random session secret and asks for an admin password.

## Development

```bash
# backend (Go 1.26+), against your real cluster
make dev-backend               # uses ~/.talos/config and ~/.kube/config, password "admin"
# frontend on http://localhost:3000, proxies /api to :8080
make dev-frontend
# tests
make test                      # Go
cd frontend && npm test        # UI helpers (node --test, Node 22.18+)
```

After the first successful `go mod tidy` (the Docker build does this), commit `backend/go.sum` and `frontend/package-lock.json` for reproducible builds.

## Ideas for next steps

- Run `talosctl upgrade-k8s` equivalent from the UI
- Scheduled etcd snapshots to `DATA_DIR` / S3
- Notifications (ntfy / Discord / email) when a health check turns red
- Certificate expiry checks, Image Factory schematic editor
