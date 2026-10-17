#!/usr/bin/env bash
# Runs before `shopify app dev` in bun run app:start:dev.
#
# When the dev app doesn't exist yet (empty client_id), the CLI creates or
# links one and rewrites shopify.app.dev.toml from the Dev Dashboard, which
# drops this template's webhooks, App Proxy and api_version. So the linking
# happens here first: the CLI writes its file, then the template's file is
# put back with only the new client_id, name and handle copied into it.
set -euo pipefail
cd "$(dirname "$0")/.."

config=shopify.app.dev.toml
grep -Eq '^client_id = ".+"' "$config" && exit 0

backup="$(mktemp)"
cp "$config" "$backup"
trap 'cp "$backup" "$config"; rm -f "$backup"' EXIT

shopify app config link --config dev

value() { sed -n "s/^$1 = \"\(.*\)\"$/\1/p" "$config" | head -1; }
client_id="$(value client_id)"
name="$(value name)"
handle="$(value handle)"
[ -n "$client_id" ] || { echo "Linking didn't produce a client_id; $config is unchanged."; exit 1; }

cp "$backup" "$config"
CLIENT_ID="$client_id" APP_NAME="$name" APP_HANDLE="$handle" perl -pi -e '
  s/^client_id = .*/client_id = "$ENV{CLIENT_ID}"/;
  s/^name = .*/name = "$ENV{APP_NAME}"/ if $ENV{APP_NAME} ne "";
  s/^handle = .*/handle = "$ENV{APP_HANDLE}"/ if $ENV{APP_HANDLE} ne "";
' "$config"
trap - EXIT
rm -f "$backup"
echo "Linked the dev app ($client_id); kept the template's config in $config."
