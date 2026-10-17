// Fails when a Shopify config file isn't valid TOML (a duplicated key, a
// typo), which `shopify app dev` otherwise only reports when someone starts
// the app. Runs in CI: bun run check:toml
const patterns = ["shopify*.toml", "extensions/*/shopify.extension.toml"];

let failed = false;
for (const pattern of patterns) {
  for (const file of new Bun.Glob(pattern).scanSync(".")) {
    try {
      Bun.TOML.parse(await Bun.file(file).text());
      console.log(`ok   ${file}`);
    } catch (error) {
      failed = true;
      console.error(`bad  ${file}: ${(error as Error).message}`);
    }
  }
}
if (failed) {
  process.exit(1);
}
