---
type: runbook
title: Deploying the hosted server
description: Run the hosted server free — Render for the service, Neon for the database — and point agents at it.
resource: render.yaml
tags: [runbook, deploy, hosted, render, neon]
timestamp: 2026-08-30
---

# Deploying the hosted server

> This runbook deploys the current v0.1 hosted implementation. It does not
> provide the adopted pure-read/public-history contract or optional offline
> Agent activation. See [status.md](../architecture/status.md).

The [deployment shape](../architecture/hosted-plan.md) settled in hosted-plan.md: the Go server runs as one stateless Render free-tier web service, the database lives on Neon free, and the management UI is a static SPA on Cloudflare Pages. This runbook covers the server and the database, then the UI.

The whole setup costs $0. The free-tier constraints that matter are recorded at the end. The production instance lives at `[redacted deployment address]` with the UI at `[redacted deployment address]` (see [the log](../log.md)).

## 0. Prerequisites

- A GitHub account (signs you into both Render and Neon).
- The tearoom binary built locally for one command: `make build` (`bin/tearoom`).
- The repository pushed to GitHub (Render deploys from it).

## 1. Neon: create the database

1. Sign in at [neon.tech](https://neon.tech) with GitHub; create a project in the region closest to your agents.
2. Copy the **pooled** connection string (DSN) — `postgresql://...?...`.

## 2. Initialize the schema

The server also migrates on start, but running `init-db` first verifies the DSN and the two migration chains:

```bash
bin/tearoom hosted init-db --database "$DATABASE_URL"
```

## 3. Generate the admin token

Phase 1 gates management routes on a dev-mode admin token (the GitHub OAuth exchange replaces it in Phase 2). Generate a strong one:

```bash
openssl rand -hex 32
```

## 4. Render: deploy the web service

1. Sign in at [render.com](https://render.com) with GitHub; create a **New Web Service** from the repository.
2. Runtime: **Docker** (uses the repo's `Dockerfile`; static `CGO_ENABLED=0` build). The image defaults to `hosted serve` (reads `$DATABASE_URL`), so a start command is optional; the blueprint sets one explicitly anyway.
3. Plan: **Free**.
4. Environment variables:

   | key | value |
   |---|---|
   | `DATABASE_URL` | the Neon DSN from step 1 |
   | `TEAROOM_HOSTED_ADMIN` | the token from step 3 |
   | `TEAROOM_HOSTED_DEV` | `1` (enables the admin routes until OAuth lands) |

5. Deploy. The service answers `GET /healthz` for the platform health check.

The repo's [render.yaml](../../render.yaml) is a blueprint for the same thing; its two `sync: false` secrets are filled in from the dashboard or `render` CLI.

Alternatively, drive everything from the API (the path this instance actually took): `RENDER_API_KEY` against `api.render.com/v1` — service type is `web_service`, and env vars are a full-replacement `PUT /v1/services/<id>/env-vars` with a bare array body. Note the API refuses service creation until the account has a payment method on file, even on the free plan; that one step stays dashboard-only.

## 5. Verify end to end

```bash
SVC=https://<service>.onrender.com
curl -s "$SVC/healthz"                                  # {"status":"ok"}

TOKEN=<admin token>
ROOM=$(curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"ops"}' "$SVC/api/hosted/rooms" | jq -r .id)
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"actor":"alice","capabilities":"read,append"}' "$SVC/api/hosted/rooms/$ROOM/credentials"
# → {"token":"tr_...", ...} — the plaintext is shown once; keep it
```

Then exercise the current v0.1 agent flow with the credential:

```bash
TEAROOM_SERVER="$SVC/r/$ROOM" TEAROOM_TOKEN=tr_... TEAROOM_ACTOR=alice \
  bin/tearoom event send "hello from the hosted room"
```

## 6. TLS and domains

`*.onrender.com` serves HTTPS by default; custom domains are a paid-plan feature on Render, so free-tier agents use the `*.onrender.com` URL. TLS termination in front of the server is the security boundary assumed in [security.md](../domain/security.md).

## 7. Management UI

The [management UI](../../web/README.md) (`web/`) is a static React/Vite SPA. Build with the Render service as the API base, then deploy `dist/` to Cloudflare Pages:

```bash
VITE_API_BASE="$SVC" npm --prefix web run build
```

The server must allow the Pages origin: set `TEAROOM_CORS_ORIGIN` (or pass `--cors-origin`) to the Pages site's origin — without it the browser is refused by CORS. The UI signs in with the same `TEAROOM_HOSTED_ADMIN` token and manages rooms and credentials over `/api/hosted/*`.

## 8. Free-tier constraints (2026)

- **750 instance hours/month** — more than any month's 24/7 needs (the longest is 744), and spun-down time does not count. Client polling keeps the instance awake while any agent is watching.
- **15-minute spin-down** — a quiet night sleeps the instance; the first request after that pays one 30–60 s cold start. Coordination tolerates this (`event watch` is a client poll, not a held connection).
- **5 GB/month egress** — legacy watch/list responses are small; for many
  v0.1 clients, raise `--poll-interval` above the 250 ms default instead of
  paying the request volume. Target `read --wait` behavior is not yet deployed.
- **Never Render's own PostgreSQL** — its free database expires 30 days after creation by design. The database is Neon's, always.
