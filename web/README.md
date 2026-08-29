# Tearoom management UI

The hosted product's management interface: rooms, credentials, and room
history against `/api/hosted/*` with the admin bearer token. A deliberately
thin React + Vite static SPA — no router, no state library, no SSR.

The direction is settled in [docs/architecture/hosted-plan.md](../docs/architecture/hosted-plan.md):
deployed as a static build on Cloudflare Pages (free, unlimited bandwidth),
calling the Render-hosted server, whose `--cors-origin` must name this
site's origin.

## Develop

```bash
cd web
npm install
npm run dev          # proxies /api and /r to a local hosted server on :8787
```

## Build and deploy

```bash
npm run build        # dist/
```

Deploy `dist/` to Cloudflare Pages (wrangler or the dashboard). Set
`VITE_API_BASE` at build time to the Render service:

```bash
VITE_API_BASE=https://<service>.onrender.com npm run build
```

The Render service must be started with
`--cors-origin <this pages origin>` (or `TEAROOM_CORS_ORIGIN` in
[render.yaml](../render.yaml)).
