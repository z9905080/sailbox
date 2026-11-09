---
name: sailbox
description: Inspect and operate a Sailbox (self-hosted PaaS on K3s) through the `sailbox` CLI - list projects and apps, trigger and watch deployments, restart apps, or call any REST endpoint. Use when the user asks to deploy, check, restart or change an app hosted on their Sailbox.
---

# Sailbox CLI

`sailbox` talks to the Sailbox REST API with the API key in `SAILBOX_API_KEY`
and the base URL in `SAILBOX_URL`. If either is missing, `sailbox status` says
so: ask the user to export them. Never ask for, print or write the key yourself.

Every command prints JSON on stdout (pipe it to `jq`), errors on stderr. List
endpoints return `{"items": [...], "pagination": {...}}`; add `?page=N&per_page=100`
via `sailbox api` to page through more than the default 20.
Exit code 0 = success, 1 = API/network error (the API's error JSON is still on
stdout), 2 = bad usage. Run `sailbox help` for the full list.

## Common tasks

```sh
sailbox status                                   # check URL + key
sailbox projects | jq '.items[] | {id, name}'         # find a project
sailbox apps --search web | jq '.items[] | {id, name, status}'
sailbox app <app-id>
sailbox deploy <app-id> --wait                   # blocks until success; exit 1 on failure
sailbox deployments <app-id> | jq '.items[0]'       # latest deployment, including its error
sailbox restart <app-id>
```

Anything else: `sailbox api <METHOD> <PATH> [-d JSON|@file|-]`, with `PATH`
relative to `/api/v1`, for example
`sailbox api POST /apps/<id>/scale -d '{"replicas":2}'`.
Endpoints follow the dashboard: `/projects`, `/apps/:id/{env,secrets,domains,pods,metrics,status}`,
`/deployments/:id/{cancel,rollback}`, `/databases`, `/cronjobs`, `/cluster/*`, `/monitoring/*`.

## Rules

- Look up IDs with `projects` / `apps`; never guess them.
- The key has its owner's full role. Before anything destructive or user-visible
  (DELETE, stop, rollback, scale to 0, changing env/secrets/domains, cluster
  cleanup), tell the user exactly what you will run and wait for a yes.
- `GET .../secrets` and `/databases/:id/credentials` return plaintext secrets:
  do not fetch them unless the user asked for that value.
- Live logs and the terminal are WebSocket/JWT-only and cannot be reached with
  the CLI; use the deployment's JSON or point the user to the dashboard.
