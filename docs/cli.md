# `sailbox` CLI

A small REST client for Sailbox, authenticated with an [API key](api-keys.md).
It is built for scripts and AI agents: every command prints the API's JSON to
stdout, errors go to stderr, and the exit code is `0` on success, `1` on an API
or network error and `2` on a usage error. It has no dependencies beyond Go's
standard library.

## Install

```sh
cd apps/cli
go build -o "$(go env GOPATH)/bin/sailbox" .
```

## Configure

```sh
export SAILBOX_URL='https://sailbox.example.com'
export SAILBOX_API_KEY='sbk_...'   # from Dashboard → API Keys; keep it in a secret manager
sailbox status                     # {"status": "ok"}
```

The key is read only from the environment, never from a flag, so it does not
end up in shell history or process lists. Plain `http://` is refused unless the
host is loopback, a private LAN address or Tailscale (`100.64.0.0/10`, `*.ts.net`);
set `SAILBOX_ALLOW_HTTP=1` to override.

## Commands

| Command | Calls |
| --- | --- |
| `sailbox status` | `GET /status` |
| `sailbox projects` | `GET /projects` |
| `sailbox apps [--project ID] [--search S] [--status S]` | `GET /apps` or `GET /projects/:id/apps` |
| `sailbox app <app-id>` | `GET /apps/:id` |
| `sailbox deploy <app-id> [--force-build] [--wait] [--timeout 15m]` | `POST /apps/:id/deploy`, then polls `GET /deployments/:id` with `--wait` (exit 1 if it fails, is cancelled or times out) |
| `sailbox deployments <app-id>` | `GET /apps/:id/deployments` |
| `sailbox deployment <id>` | `GET /deployments/:id` |
| `sailbox restart <app-id>` | `POST /apps/:id/restart` |
| `sailbox api <METHOD> <PATH> [-d JSON\|@file\|-]` | Any endpoint; `PATH` is relative to `/api/v1` |

Anything without a shortcut goes through `api`:

```sh
sailbox api POST /apps/<id>/scale -d '{"replicas": 2}'
sailbox api PUT /apps/<id>/env -d @env.json
sailbox api GET '/deployments?status=failed'
```

The key carries its owner's full role (see [api-keys.md](api-keys.md)), so an
agent using an admin's key can delete things. Give agents a key from a user with
only the project access they need, set a short expiry, and revoke it afterwards.
WebSocket endpoints (live logs, terminal) are JWT-only and not available here.

## Using it from an AI agent

[`apps/cli/skill/sailbox/SKILL.md`](../apps/cli/skill/sailbox/SKILL.md) is a
Claude Code / Agent Skills file describing the CLI. Copy the `sailbox` folder to
`~/.claude/skills/` (or your agent's skills directory) and export the two
environment variables in the agent's environment.
