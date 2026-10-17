#!/usr/bin/env bash
# Stamp a new app out of this template:
#
#   ./scripts/new-app.sh ../My-App github.com/you/my-app "My App"
#
# Copies the template next to it, rewrites the Go module path, the app
# name and handle in both toml files and the frontend name. The copy has
# no git history; `git init` it yourself when ready.
#
# In a clone made with GitHub's "Use this template", rename in place:
#
#   ./scripts/new-app.sh . github.com/you/my-app "My App"
set -euo pipefail
cd "$(dirname "$0")/.."

if [ $# -ne 3 ]; then
  echo "usage: $0 <target-dir | .> <module-path> <app-name>"
  echo "   eg: $0 ../My-App github.com/you/my-app \"My App\""
  exit 1
fi

target=$1
module=$2
name=$3
handle=$(echo "$name" | tr '[:upper:] ' '[:lower:]-' | tr -cd 'a-z0-9-')

if [ "$target" != "." ]; then
  [ -e "$target" ] && { echo "$target already exists"; exit 1; }
  mkdir -p "$target"
  # Everything except local state; dist stays so `go build` works at once.
  rsync -a --exclude .git --exclude .env --exclude .shopify \
    --exclude web/frontend/node_modules ./ "$target/"
  cd "$target"
fi

old_module="github.com/ChristiantangCC/Shopify-Go-App-Template"
grep -rl "$old_module" go.mod ./cmd ./internal ./web/embed.go \
  | OLD="$old_module" NEW="$module" xargs perl -pi -e 's/\Q$ENV{OLD}\E/$ENV{NEW}/g'

APP_NAME="$name" APP_HANDLE="$handle" perl -pi -e '
  s/^name = .*/name = "$ENV{APP_NAME}"/;
  s/^handle = .*/handle = "$ENV{APP_HANDLE}"/;
  s/^client_id = .*/client_id = ""/;
  s/^subpath = .*/subpath = "$ENV{APP_HANDLE}"/;
' shopify.app.toml

# The dev app gets its own App Proxy subpath too: a subpath belongs to one
# app per store, and the dev store usually has other dev apps installed.
APP_NAME="$name (dev)" APP_HANDLE="$handle-dev" perl -pi -e '
  s/^name = .*/name = "$ENV{APP_NAME}"/;
  s/^handle = .*/handle = "$ENV{APP_HANDLE}"/;
  s/^client_id = .*/client_id = ""/;
  s/^subpath = .*/subpath = "$ENV{APP_HANDLE}"/;
' shopify.app.dev.toml

APP_HANDLE="$handle" perl -pi -e 's/"name": "shopify-go-app-template"/"name": "$ENV{APP_HANDLE}"/' package.json
APP_HANDLE="$handle" perl -pi -e 's/"name": "shopify-go-app-template-frontend"/"name": "$ENV{APP_HANDLE}-frontend"/' web/frontend/package.json
APP_NAME="$name" perl -pi -e 's/<title>.*<\/title>/<title>$ENV{APP_NAME}<\/title>/' web/frontend/index.html

go build ./... >/dev/null

echo "Created $target"
echo "  module  $module"
echo "  name    $name (handle: $handle)"
echo
echo "Next: bun run app:start:dev"
echo "The Shopify CLI creates the dev app and installs it on your dev store."
