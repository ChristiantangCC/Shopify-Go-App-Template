# Rotating `ENCRYPTION_KEY`

Access and refresh tokens are encrypted at rest with AES-256-GCM
(`internal/crypto`). Rotate the key when it may have leaked, when someone
who had it leaves, or on a schedule. Shops keep working throughout: nobody
has to reopen the app.

## How it works

- Every sealed value records which key encrypted it:
  `v2:<key id>:<ciphertext>`. The key id is the first 8 hex characters of
  the key's SHA-256, safe to log. The server logs the current one at boot
  (`[Server] Encryption ready key=…`).
- `ENCRYPTION_KEY` is the current key: every new or refreshed token is
  sealed with it.
- `ENCRYPTION_KEYS_OLD` holds earlier keys, comma separated. They are only
  used to decrypt.
- The `reencrypt-tokens` job runs at every boot and daily after that. It
  finds shops whose tokens aren't sealed with the current key and rewrites
  them under the shop's lock, so it never races a token refresh.
- Values in the old `v1:` format (before key ids existed) are opened by
  trying each key, and the job rewrites them to `v2` like any other stale
  value.

## Steps

1. Generate a new key:

   ```bash
   openssl rand -hex 32
   ```

2. In the deployment's environment, move the current key to
   `ENCRYPTION_KEYS_OLD` and set the new one as `ENCRYPTION_KEY`:

   ```bash
   ENCRYPTION_KEY=<new key>
   ENCRYPTION_KEYS_OLD=<previous key>
   ```

   If an earlier rotation's key is still listed, keep it until step 4 has
   confirmed nothing needs it.

3. Deploy. On boot the job re-encrypts every shop and logs:

   ```
   [Jobs] Tokens re-encrypted key=<new id> resealed=<n> failed=0
   [Jobs] Every token is sealed with the current key key=<new id>
   ```

4. Once that second line appears, nothing needs the old key any more.
   Remove `ENCRYPTION_KEYS_OLD` and deploy again. Then destroy the old key.

## When something fails

- **`failed` is above 0.** The job logs each shop with
  `[Jobs] Re-encrypt failed`. The usual cause is a value sealed with a key
  that isn't configured: the error names the missing key id. Add that key
  to `ENCRYPTION_KEYS_OLD` and restart. Without that key the shop recovers
  on its own the next time a merchant opens the app: unreadable tokens are
  treated like expired ones and replaced by a new token exchange. Until
  then, background work for that shop fails.
- **The old key was removed too early.** Background work for affected
  shops fails with `sealed with key <id>, which is not configured`, and
  each shop needs a merchant to open the app before it works again. Put
  the key back in `ENCRYPTION_KEYS_OLD` and restart; the job finishes the
  move.
- **The key leaked together with a database copy.** Rotating protects
  what is written from now on, not the copy. Its access tokens expire
  within about an hour; its refresh tokens stop working once the app
  refreshes that shop's token, because every refresh rotates the pair.
