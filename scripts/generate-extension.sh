#!/usr/bin/env bash
# Generates a Shopify extension: bun run generate:extension
#
# `shopify app generate extension` installs the new extension's packages
# with npm, which crashes on bun's node_modules layout. So the generator
# runs in a clean temporary folder holding only the app config, and the
# extension is moved in here afterwards as a bun workspace. Any arguments
# are passed to the generator, e.g. --template checkout_ui --name upsell.
set -euo pipefail
cd "$(dirname "$0")/.."

if ! grep -Eq '^client_id = ".+"' shopify.app.dev.toml; then
  echo "Run bun run app:start:dev once first: it creates the dev app extensions belong to."
  exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cp shopify.app.dev.toml shopify.app.toml "$work/"
printf '{\n  "name": "generate",\n  "private": true\n}\n' > "$work/package.json"

(cd "$work" && shopify app generate extension --config dev "$@")

for generated in "$work"/extensions/*/; do
  name="$(basename "$generated")"
  if [ -e "extensions/$name" ]; then
    echo "extensions/$name already exists; nothing was changed."
    exit 1
  fi
  rm -rf "$generated/node_modules" "$generated/package-lock.json"
  # Some templates put the extension's packages in the app's root
  # package.json; here every extension carries its own.
  if [ ! -f "$generated/package.json" ]; then
    NAME="$name" ROOT="$work/package.json" OUT="$generated/package.json" bun -e '
      const root = await Bun.file(process.env.ROOT).json();
      const pkg = { name: process.env.NAME, private: true };
      if (root.dependencies) pkg.dependencies = root.dependencies;
      if (root.devDependencies) pkg.devDependencies = root.devDependencies;
      await Bun.write(process.env.OUT, JSON.stringify(pkg, null, 2) + "\n");
    '
  fi
  cp -R "$generated" "extensions/$name"
  echo "Added extensions/$name"
done

bun install
