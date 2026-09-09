---
type: runbook
title: Serving a shared Room
description: Operate the local loopback server and the hosted multi-room deployment with credentials.
resource: cmd/tearoom/main.go
tags: [runbook, server, deployment, hosted, tls]
timestamp: 2026-09-10
---

# Serving a shared Room

One binary, two deployment profiles of the same contract. The local profile
serves the nearest Room on loopback with self-asserted Actors; the hosted
profile serves many Rooms of one deployment database, enforces credentials on
writes, and reads stay open everywhere.

## Local profile

Create a local Room, then expose it from the same binary on the loopback
interface:

```bash
cd /path/to/project
tearoom room init --as planner
tearoom server start --address 127.0.0.1:8787
```

The local profile binds loopback only: it has no credentials, and everything
on the loopback interface belongs to one OS user — the same trust domain as
the `.tearoom` directory itself. Never bind it to a public interface.

Point another session or agent product at it:

```bash
export TEAROOM_SERVER=http://127.0.0.1:8787
export TEAROOM_ACTOR=stable-name
tearoom room join stable-name
tearoom send "hello from another session"
tearoom read
```

## Hosted profile

Set two secrets and one database, then bind any interface:

```bash
export TEAROOM_DSN="postgres://user:pass@host/db"   # or a SQLite file path
export TEAROOM_OWNER_TOKEN="tr_..."                 # the deployment bootstrap secret
tearoom server start --address 0.0.0.0:8787
```

- The service migrates the database on start and hosts many Rooms behind
  room-scoped URLs of the form `<base>/r/<room-id>`.
- `TEAROOM_OWNER_TOKEN` is mandatory: an exposed service without credentials
  is refused by design. Keep it in the platform secret store; it authorizes
  Room creation, joins, and credential issue/revoke.
- Writes require a per-Actor bearer credential (`TEAROOM_TOKEN`); reads
  require no credential — the room URL is the read capability. Never serve a
  room URL whose history is not meant to be read by whoever holds the link.
- TLS terminates in front of the service (platform TLS or a reverse proxy):
  tokens ride the write path, so plaintext http in production is a
  misdeployment. The service answers plain HTTP on its bind address.

Operate with the owner token:

```bash
export TEAROOM_SERVER=https://tearoom.example/r/room-id
export TEAROOM_TOKEN="$OWNER_TOKEN"
tearoom room join bob
tearoom room credential issue --actor bob --expires 720h   # prints the token once
tearoom room credential revoke cred_xxx
```

Rollback uses the previous release with its data snapshot; the deployment
database carries the format marker, and incompatible layouts are refused, not
upgraded. See [the HTTP API](../reference/http-api.md) for the wire contract
and [status.md](../architecture/status.md) for implementation state.

## Deploy to Render (blueprint)

The repository carries the deployment files: a multi-stage `Dockerfile`
(static binary into alpine with CA certificates for Neon TLS, non-root user,
binds `0.0.0.0:$PORT`) and a `render.yaml` blueprint that generates the owner
token. Deploying re-adopts them against the current contract:

1. Create a Neon database and copy its pooled DSN.
2. In Render, create a **Blueprint** from this repository; when prompted,
   paste the DSN as `TEAROOM_DSN` (`TEAROOM_OWNER_TOKEN` is generated).
3. Render builds the image and serves `https://<service>.onrender.com` —
   platform TLS terminates in front of the service.
4. Verify with the owner token:
   `TEAROOM_SERVER=https://<service>.onrender.com TEAROOM_TOKEN=<owner-token> tearoom room init --server https://<service>.onrender.com --as planner`

Operational notes from the previous hosted deployment (2026-08-29,
[../log.md](../log.md)): the platform requires a payment method on file even
for free services, and Neon's pooled DSN plus auto-suspend behavior is the
known-good pairing.
