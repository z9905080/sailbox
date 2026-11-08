# REST API keys

Owners and admins can open **API Keys** in the dashboard sidebar (`/api-keys`).
Create a named key, optionally set an expiry (shown in your browser's local time),
and copy it from the one-time dialog. Closing the dialog forgets the plaintext;
it cannot be retrieved again. Use **Edit** to rename or replace/remove expiry,
and **Revoke** to permanently disable a key. Only your own keys are listed and
manageable, even when you are an organization owner. Revocation is irreversible.

Keys start with `sbk_` and contain 256 bits of cryptographically random material.
The database stores only a SHA-256 hash and metadata, never the plaintext.
Generated plaintext is returned only by the create response (`key`); list responses
contain metadata only. The UI does not store keys in localStorage or query caches.
Creation responses have `Cache-Control: no-store`. Protect database backups too:
they contain API-key metadata and hashes (not the keys themselves).

## Calling the API

Use HTTPS outside localhost. Put the secret in your secret manager/environment;
do not paste keys into URLs, logs, scripts committed to Git, or shared terminals.
The following assumes `SAILBOX_API_KEY` is already securely set:

```sh
export SAILBOX_URL='https://sailbox.example.com'
curl --fail-with-body --silent --show-error \
  --header "Authorization: Bearer ${SAILBOX_API_KEY}" \
  "${SAILBOX_URL}/api/v1/status"
# {"status":"ok"}

# Alternative header (send only one authentication header):
curl --fail-with-body --silent --show-error \
  --header "X-API-Key: ${SAILBOX_API_KEY}" \
  "${SAILBOX_URL}/api/v1/projects"
```

Expired, revoked, malformed, unknown keys and missing/deleted owners return 401.
Sending both authentication headers or duplicate auth headers is rejected. There
is no fallback from an invalid key to JWT. Existing Bearer JWT authentication and
session-revocation checks continue to work. WebSocket/SSE endpoints remain JWT-only;
API keys are **never accepted as query parameters**.

Keys inherit their owner's **current** user/organization role and the existing
project membership/read-only rules; they do not bypass any guards. They are not
independently scoped read-only keys: an admin's key can perform administrative
REST operations. Use short expiries and revoke unused keys. Password, 2FA and role
changes that increment the user's existing `token_version` invalidate all their
previous keys; create new keys after such changes. The dashboard's Active badge
indicates the key's active/expiry metadata, not a promise that its owner's session
version is still current. Owner lookup and key validity are checked on each request,
without a revocation cache. Last-use metadata is best-effort, written at most once
per minute to keep the footprint small. No additional services are required.

API keys cannot manage API keys or change account profile/password/2FA. These
operations require a normal dashboard JWT session. Other existing admin endpoints
retain their usual permissions; API keys are powerful credentials, not sandboxes.

## Management endpoints (JWT + owner/admin required)

| Method | Path | Body / response |
| --- | --- | --- |
| GET | `/api/v1/api-keys` | Array of your key metadata |
| POST | `/api/v1/api-keys` | `{ "name": "automation", "expires_at": "2099-01-01T00:00:00Z" }`; 201 `{ "api_key": { ...metadata }, "key": "<one-time-secret>" }` |
| PUT | `/api/v1/api-keys/:id` | Replace name and expiry; 204 |
| DELETE | `/api/v1/api-keys/:id` | Permanently revoke; 204 |

Names must have 1–100 characters after trimming. Expiry must be in the future,
RFC 3339 with a timezone; `null` (or omitted on PUT) removes expiry. The owner is
always derived from the authenticated session, not a submitted `user_id`.
Revoked keys cannot be edited or reactivated. Unknown/other users' IDs return 404.

## Database upgrade / rollback

The registered Bun migration `20261108_006_add_api_keys.go` adds `api_keys` with a
user foreign key, unique hash, expiry, activity metadata and session version.
Existing tables/data are unchanged. The normal API startup automatically runs
pending migrations under its existing migration lock. Back up the database and
test the new image against a disposable database before any production upgrade.
To apply explicitly, use the existing configured environment from `apps/api`:

```sh
go run ./cmd/migrate status
go run ./cmd/migrate up
```

The down migration drops `api_keys` and permanently discards every key/hash.
The existing `go run ./cmd/migrate rollback` rolls back the **last migration group**,
not necessarily just this migration. Only use it when this is the sole migration
in that group, after checking status and taking a backup. Roll back the application
version together with the schema. Reapplying creates an empty key table; users
must create new keys. No database migration was applied to your live server by
this implementation task.

## Developer tests

```sh
cd apps/api
go test ./...
go test -race ./internal/auth ./internal/api/middleware ./internal/server
go vet ./...
cd ../web
bun run build
bun x tsc --noEmit
```

The production-router tests use isolated in-memory stores and exercise the full
key lifecycle, JWT regression/revocation, malformed/expired/revoked keys, owner
removal/version changes, admin-only own-key management and project RBAC.

The PostgreSQL integration test is opt-in. Supply an existing **disposable local**
database named `sailbox_api_keys_test*` with a role allowed to create schemas:

```sh
# SAILBOX_TEST_DATABASE_URL must already point to that disposable localhost DB.
cd apps/api
go test -v ./internal/store/pg -run TestAPIKeyStorePostgres
```

It creates/drops only a unique test schema; it checks migrations up/down/reapply,
PostgreSQL persistence, owner isolation, irreversible revocation, throttled
last-use updates and user-delete cascade. Without `SAILBOX_TEST_DATABASE_URL` it
is explicitly skipped. Do not point this or any test at production.
