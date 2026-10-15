#!/usr/bin/env bash
# Run BY `shopify app dev` (wired up in shopify.web.toml), not by hand.
# The CLI opens the tunnel, keeps the app URL updated, picks the dev store,
# installs the app and provides the preview/GraphiQL hotkeys. It hands this
# script the tunnel URL and credentials as env vars (HOST, SHOPIFY_API_KEY,
# SHOPIFY_API_SECRET, PORT); this script adds Postgres, Vite and the Go
# server on top.
set -euo pipefail
cd "$(dirname "$0")/.."

touch .env
get() { sed -n "s/^$1=//p" .env | tail -1; }
put() {
  if grep -q "^$1=" .env; then
    KEY="$1" VALUE="$2" perl -pi -e 's/^$ENV{KEY}=.*/$ENV{KEY}=$ENV{VALUE}/' .env
  else
    echo "$1=$2" >> .env
  fi
}

# Persist what the CLI handed us, so workers, tests and scripts that run
# outside `shopify app dev` see the same values.
put HOST "${HOST:?missing HOST, run this through: pnpm app:start:dev}"
put SHOPIFY_CLIENT_ID "${SHOPIFY_API_KEY:?missing SHOPIFY_API_KEY}"
put SHOPIFY_CLIENT_SECRET "${SHOPIFY_API_SECRET:?missing SHOPIFY_API_SECRET}"
[ -n "$(get ENCRYPTION_KEY)" ] || put ENCRYPTION_KEY "$(openssl rand -hex 32)"

# Postgres: reuse the running container's port, otherwise find a free one
# (another project often holds 5432 already).
port=""
if [ -n "$(docker compose ps --status running -q postgres 2>/dev/null)" ]; then
  port=$(docker compose port postgres 5432 | sed 's/.*://')
fi
if [ -z "$port" ]; then
  port=$(get POSTGRES_PORT)
  port=${port:-5432}
  while lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; do
    port=$((port + 1))
  done
fi
put POSTGRES_PORT "$port"
POSTGRES_PORT=$port docker compose up -d --wait postgres

# Keep DATABASE_URL on the chosen port, unless it points somewhere custom.
if [ -z "$(get DATABASE_URL)" ] || get DATABASE_URL | grep -q '^postgres://postgres:postgres@localhost:'; then
  put DATABASE_URL "postgres://postgres:postgres@localhost:$port/app?sslmode=disable"
fi

# Vite for frontend hot reload; the Go server proxies HTML to it.
[ -d web/frontend/node_modules ] || (cd web/frontend && pnpm install)
(cd web/frontend && exec pnpm dev >/dev/null 2>&1) &
vite_pid=$!
trap 'kill "$vite_pid" 2>/dev/null || true' EXIT

# The Go server must listen on the port the CLI proxies the tunnel to.
DATABASE_URL="$(get DATABASE_URL)" \
ENCRYPTION_KEY="$(get ENCRYPTION_KEY)" \
SHOPIFY_CLIENT_ID="$SHOPIFY_API_KEY" \
SHOPIFY_CLIENT_SECRET="$SHOPIFY_API_SECRET" \
HOST="$HOST" \
PORT="${PORT:-8080}" \
FRONTEND_DEV_URL="http://localhost:5173" \
LOG_FORMAT=text \
  go run ./cmd/server
