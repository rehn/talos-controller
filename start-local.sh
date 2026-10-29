#!/usr/bin/env bash
#
# Start Talos Controller locally on your Mac (or any Linux box).
#
#   ./start-local.sh            same as "docker"
#   ./start-local.sh docker     build and run the Docker image  -> http://localhost:8080
#   ./start-local.sh dev        Go backend + Next.js dev server  -> http://localhost:3000 (hot reload)
#   ./start-local.sh single     build UI, embed it, run Go only   -> http://localhost:8080 (no Docker)
#   ./start-local.sh stop       stop the Docker container
#
# Options (any mode, any order):
#   -t, --talosconfig PATH   talosconfig file to use   (optional: you can also upload it in the UI under Settings)
#   -k, --kubeconfig PATH    kubeconfig to use         (default: saved choice, ./config/kubeconfig, ~/.kube/config)
#       --no-kubeconfig      don't use a kubeconfig; fetch it through the Talos API instead
#   -c, --context NAME       talosconfig context to use
#
# Paths given with -t/-k are remembered in .env for the next run (and used by docker compose).
# Example:  ./start-local.sh dev -t ~/clusters/home/talosconfig
#
# Written for macOS's default bash 3.2, so no bash 4 features.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

MODE=""
ARG_TALOS=""
ARG_KUBE=""
ARG_CONTEXT=""
NO_KUBE=0

usage() { sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
  case "$1" in
    -t|--talosconfig) [ $# -ge 2 ] || { echo "$1 needs a path" >&2; exit 1; }; ARG_TALOS="$2"; shift 2 ;;
    --talosconfig=*)  ARG_TALOS="${1#*=}"; shift ;;
    -k|--kubeconfig)  [ $# -ge 2 ] || { echo "$1 needs a path" >&2; exit 1; }; ARG_KUBE="$2"; shift 2 ;;
    --kubeconfig=*)   ARG_KUBE="${1#*=}"; shift ;;
    --no-kubeconfig)  NO_KUBE=1; shift ;;
    -c|--context)     [ $# -ge 2 ] || { echo "$1 needs a name" >&2; exit 1; }; ARG_CONTEXT="$2"; shift 2 ;;
    --context=*)      ARG_CONTEXT="${1#*=}"; shift ;;
    -h|--help|help)   usage; exit 0 ;;
    -*)               echo "Unknown option $1 (see --help)" >&2; exit 1 ;;
    *)                [ -z "$MODE" ] || { echo "Only one mode allowed (got '$MODE' and '$1')" >&2; exit 1; }; MODE="$1"; shift ;;
  esac
done
MODE="${MODE:-docker}"

bold() { printf '\033[1m%s\033[0m\n' "$*"; }
info() { printf '  \033[34m›\033[0m %s\n' "$*"; }
warn() { printf '  \033[33m!\033[0m %s\n' "$*"; }
die()  { printf '  \033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is not installed. $2"
}

port_free() {
  if command -v lsof >/dev/null 2>&1 && lsof -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1; then
    die "Port $1 is already in use (stop whatever is running there, or './start-local.sh stop')."
  fi
}

# sed -i differs between macOS and Linux
sed_inplace() {
  if sed --version >/dev/null 2>&1; then sed -i "$@"; else sed -i '' "$@"; fi
}

# Read a single KEY=value from .env without sourcing it (bcrypt hashes contain $).
env_get() {
  [ -f .env ] || return 0
  { grep -E "^$1=" .env || true; } | tail -n1 | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//' -e 's/\$\$/$/g'
}

# Set KEY=value in .env (replace or append).
env_set() {
  local val
  val=$(printf '%s' "$2" | sed -e 's/[\\/&|]/\\&/g' -e 's/\$/$$/g')
  if grep -qE "^$1=" .env 2>/dev/null; then
    sed_inplace "s|^$1=.*|$1=${val}|" .env
  else
    printf '%s=%s\n' "$1" "$(printf '%s' "$2" | sed 's/\$/$$/g')" >> .env
  fi
}

# Expand a leading ~ and make the path absolute.
abspath() {
  local p="${1/#\~/$HOME}"
  [ -e "$p" ] || { echo "$p"; return 0; }
  if [ -d "$p" ]; then (cd "$p" && pwd); else printf '%s/%s\n' "$(cd "$(dirname "$p")" && pwd)" "$(basename "$p")"; fi
}

# Resolved paths, set by setup_config.
TALOS_FILE=""
KUBE_FILE=""

# ---------------------------------------------------------------------------
# Shared setup: data/, .env, which talosconfig/kubeconfig to use
# ---------------------------------------------------------------------------
setup_config() {
  bold "Preparing configuration"
  mkdir -p data

  if [ ! -f .env ]; then
    cp .env.example .env
    sed_inplace "s/^SESSION_SECRET=.*/SESSION_SECRET=$(openssl rand -hex 32)/" .env
    sed_inplace "s/^PUID=.*/PUID=$(id -u)/" .env
    sed_inplace "s/^PGID=.*/PGID=$(id -g)/" .env

    printf '  Choose an admin password for the web UI (leave empty for "admin"): '
    read -r -s PW || true
    echo
    PW="${PW:-admin}"
    # Escape characters that are special to sed and to docker compose (.env $ -> $$).
    PW_ESC=$(printf '%s' "$PW" | sed -e 's/[\\/&|]/\\&/g' -e 's/\$/$$/g')
    sed_inplace "s|^ADMIN_PASSWORD=.*|ADMIN_PASSWORD=${PW_ESC}|" .env
    info "Created .env (edit it later to change settings)"
  else
    info "Using existing .env"
  fi

  # --- talosconfig: flag > saved in .env > $TALOSCONFIG > ./config/talosconfig > ~/.talos/config
  local cand
  if [ -n "$ARG_TALOS" ]; then
    TALOS_FILE=$(abspath "$ARG_TALOS")
    [ -f "$TALOS_FILE" ] || die "talosconfig not found: $TALOS_FILE"
    env_set TALOSCONFIG_FILE "$TALOS_FILE"
  else
    local saved
    saved=$(env_get TALOSCONFIG_FILE)
    if [ -n "$saved" ] && [ "$saved" != "/dev/null" ] && [ ! -f "$(abspath "$saved")" ]; then
      warn "Saved talosconfig $saved not found (or it is a directory), ignoring it"
      env_set TALOSCONFIG_FILE /dev/null
      saved=""
    fi
    for cand in "$saved" "${TALOSCONFIG:-}" "$ROOT/config/talosconfig" "$HOME/.talos/config"; do
      if [ -n "$cand" ] && [ -f "$(abspath "$cand")" ]; then TALOS_FILE=$(abspath "$cand"); break; fi
    done
  fi
  if [ -n "$TALOS_FILE" ]; then
    info "talosconfig: $TALOS_FILE"
  else
    warn "No talosconfig file; add one in the web UI under Settings (or use --talosconfig PATH)"
  fi

  # --- kubeconfig: --no-kubeconfig > flag > saved > ./config/kubeconfig > ~/.kube/config (optional)
  if [ "$NO_KUBE" = 1 ]; then
    KUBE_FILE=""
    env_set KUBECONFIG_FILE /dev/null
  elif [ -z "$ARG_KUBE" ] && [ "$(env_get KUBECONFIG_FILE)" = "/dev/null" ]; then
    KUBE_FILE=""   # --no-kubeconfig was chosen earlier
  elif [ -n "$ARG_KUBE" ]; then
    KUBE_FILE=$(abspath "$ARG_KUBE")
    [ -f "$KUBE_FILE" ] || die "kubeconfig not found: $KUBE_FILE"
    env_set KUBECONFIG_FILE "$KUBE_FILE"
  else
    for cand in "$(env_get KUBECONFIG_FILE)" "$ROOT/config/kubeconfig" "$HOME/.kube/config"; do
      if [ -n "$cand" ] && [ -f "$(abspath "$cand")" ]; then KUBE_FILE=$(abspath "$cand"); break; fi
    done
  fi
  if [ -n "$KUBE_FILE" ]; then
    info "kubeconfig:  $KUBE_FILE"
  else
    warn "No kubeconfig; it will be fetched through the Talos API (needs an os:admin talosconfig)."
  fi

  # --- context
  if [ -n "$ARG_CONTEXT" ]; then
    env_set TALOS_CONTEXT "$ARG_CONTEXT"
  fi
  local ctx
  ctx=$(env_get TALOS_CONTEXT)
  [ -z "$ctx" ] || info "context:     $ctx"

  # docker compose reads these from the environment (overrides .env).
  export TALOSCONFIG_FILE="${TALOS_FILE:-/dev/null}"
  export KUBECONFIG_FILE="${KUBE_FILE:-/dev/null}"
}

check_cluster_reachable() {
  # Best effort: warn if the first Talos endpoint can't be reached on :50000.
  local ep
  [ -n "$TALOS_FILE" ] || return 0
  ep=$(awk '/endpoints:/ { if ($0 ~ /\[/) { print; exit } f = 1; next } f && /^[[:space:]]*-/ { print; exit }' "$TALOS_FILE" 2>/dev/null \
       | sed -e 's/.*\[//' -e 's/^[[:space:]]*-[[:space:]]*//' -e 's/[],].*//' -e "s/[\"' ]//g" -e 's/:.*//' | head -n1)
  [ -n "$ep" ] || return 0
  if command -v nc >/dev/null 2>&1; then
    if nc -z -w 3 "$ep" 50000 >/dev/null 2>&1; then
      info "Talos endpoint $ep:50000 is reachable"
    else
      warn "Can't reach Talos endpoint $ep:50000 from this machine (VPN / same network?)"
    fi
  fi
}

# ---------------------------------------------------------------------------
# Modes
# ---------------------------------------------------------------------------
run_docker() {
  need docker "Install Docker Desktop: https://www.docker.com/products/docker-desktop/"
  docker info >/dev/null 2>&1 || die "Docker is not running. Start Docker Desktop and try again."
  setup_config
  check_cluster_reachable
  if [ -z "$(docker compose ps -q 2>/dev/null)" ]; then port_free 8080; fi

  bold "Building and starting the container (first build takes a few minutes)"
  docker compose up -d --build

  info "Waiting for the app to become healthy…"
  for _ in $(seq 1 30); do
    if curl -fs http://localhost:8080/healthz >/dev/null 2>&1; then
      bold "✓ Talos Controller is running at http://localhost:8080"
      info "Logs:  docker compose logs -f"
      info "Stop:  ./start-local.sh stop"
      command -v open >/dev/null 2>&1 && open http://localhost:8080 || true
      return 0
    fi
    sleep 2
  done
  warn "The app did not answer within 60s. Recent logs:"
  docker compose logs --tail 40
  exit 1
}

prepare_go() {
  need go "Install with: brew install go"
  if [ ! -f backend/go.sum ]; then
    bold "Resolving Go modules (first run only)"
    (cd backend && go mod tidy)
  fi
}

prepare_node() {
  need node "Install with: brew install node"
  need npm "Install with: brew install node"
  if [ ! -d frontend/node_modules ]; then
    bold "Installing frontend dependencies (first run only)"
    (cd frontend && npm install --no-audit --no-fund)
  fi
}

backend_env() {
  export TALOSCONFIG="${TALOS_FILE:-/nonexistent}"
  export KUBECONFIG="${KUBE_FILE:-/nonexistent}"
  export DATA_DIR="$ROOT/data"
  export LISTEN_ADDR="127.0.0.1:8080"
  export ADMIN_USER="$(env_get ADMIN_USER)"; ADMIN_USER="${ADMIN_USER:-admin}"
  export ADMIN_PASSWORD="$(env_get ADMIN_PASSWORD)"
  export ADMIN_PASSWORD_BCRYPT="$(env_get ADMIN_PASSWORD_BCRYPT)"
  export SESSION_SECRET="$(env_get SESSION_SECRET)"
  export TALOS_CONTEXT="$(env_get TALOS_CONTEXT)"
  export GITHUB_TOKEN="$(env_get GITHUB_TOKEN)"
  if [ -z "$ADMIN_PASSWORD" ] && [ -z "$ADMIN_PASSWORD_BCRYPT" ]; then
    export ADMIN_PASSWORD=admin
  fi
}

run_dev() {
  setup_config
  check_cluster_reachable
  prepare_go
  prepare_node
  port_free 8080
  port_free 3000
  backend_env

  bold "Building Go backend"
  (cd backend && go build -o "$ROOT/data/talos-controller-dev" .)

  bold "Starting Go backend on :8080"
  "$ROOT/data/talos-controller-dev" &
  BACKEND_PID=$!
  trap 'echo; info "Stopping backend"; kill $BACKEND_PID 2>/dev/null || true; wait $BACKEND_PID 2>/dev/null || true' EXIT INT TERM

  for _ in $(seq 1 60); do
    curl -fs http://127.0.0.1:8080/healthz >/dev/null 2>&1 && break
    kill -0 $BACKEND_PID 2>/dev/null || die "Backend exited, see the output above."
    sleep 1
  done

  bold "Starting Next.js dev server → http://localhost:3000  (Ctrl+C stops both)"
  (sleep 4; command -v open >/dev/null 2>&1 && open http://localhost:3000) >/dev/null 2>&1 &
  cd frontend
  BACKEND_URL=http://127.0.0.1:8080 npm run dev
}

run_single() {
  setup_config
  check_cluster_reachable
  prepare_go
  prepare_node
  port_free 8080
  backend_env

  bold "Building the UI"
  (cd frontend && npm run build)
  find backend/web/dist -mindepth 1 -maxdepth 1 ! -name index.html -exec rm -rf {} + 2>/dev/null || true
  cp -R frontend/out/. backend/web/dist/

  bold "Building Go backend"
  (cd backend && go build -o "$ROOT/data/talos-controller-dev" .)

  bold "Starting Talos Controller → http://localhost:8080  (Ctrl+C to stop)"
  (sleep 3; command -v open >/dev/null 2>&1 && open http://localhost:8080) >/dev/null 2>&1 &
  exec "$ROOT/data/talos-controller-dev"
}

case "$MODE" in
  docker) run_docker ;;
  dev)    run_dev ;;
  single) run_single ;;
  stop)   docker compose down && info "Stopped" ;;
  *) die "Unknown mode '$MODE'. Use: docker | dev | single | stop" ;;
esac
