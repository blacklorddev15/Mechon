# Running the panel on Heroku

Heroku can host **the panel** — the web UI, the API and the agent hub. It **cannot host bots**.

A Mechon install is two parts, and only one of them fits in a dyno:

| Part | Runs on | Heroku? |
|---|---|---|
| `mechon` (panel) | One binary + Postgres, listens on `$PORT` | **Yes** |
| `mechon-agent` (node) | Root, Docker, `losetup`, `mkfs.ext4`, `iptables`, systemd | **No** |
| Bots | One sandboxed container with a loop-mounted disk image per bot | **No** |

Dynos are unprivileged, ephemeral, have no Docker daemon or loop devices, and no persistent disk.
So a Heroku install is always: **panel on Heroku, at least one node on a real server**. Without an
online node the panel works, but the Deploy button fails with `no_capacity`.

## What the image changes

The panel was already PaaS-friendly in one respect (`MECHON_LISTEN`) and not in others. This image
and the config changes address that:

- `MECHON_LISTEN` falls back to `0.0.0.0:$PORT`.
- `MECHON_DATABASE_URL` falls back to `$DATABASE_URL`, and `sslmode=require` is added when the URL
  points at a network host without one (Heroku Postgres refuses plaintext).
- `MECHON_PUBLIC_URL` falls back to `https://$HEROKU_APP_DEFAULT_DOMAIN_NAME`.
- `MECHON_DATA_DIR` falls back to `/tmp/mechon-data` (the only writable path in a dyno), and the
  panel logs a warning that artifacts are ephemeral.
- `MECHON_TRUST_PROXY` defaults to `true`, because the Heroku router always sits in front.

## Deploy, step by step

```bash
# 1. App on the container stack
heroku create my-panel --stack container

# 2. Database
heroku addons:create heroku-postgresql:essential-0 --app my-panel

# 3. Secret key — generate it locally and keep a copy
openssl rand -base64 32
heroku config:set --app my-panel \
  MECHON_SECRET_KEY='paste-the-base64-key'

# Public URL. Heroku suffixes the default domain (my-panel-f47059b55334.herokuapp.com),
# so copy it from `heroku apps:info` rather than guessing from the app name. Omit the var
# entirely and the panel derives it from HEROKU_APP_DEFAULT_DOMAIN_NAME, which is correct.
heroku config:set --app my-panel MECHON_PUBLIC_URL=https://my-panel-f47059b55334.herokuapp.com

# 4. Deploy (heroku.yml tells Heroku to build the Dockerfile)
git push heroku main

# 5. First admin
heroku run --app my-panel /usr/local/bin/mechon init \
  --email you@example.com --name "Your Name"
```

Then open `https://my-panel.herokuapp.com`. Health check, unauthenticated:

```bash
curl -s https://my-panel.herokuapp.com/api/v1/health
# {"status":"ok"}
```

That endpoint also checks Postgres, so it is the one to wire into an uptime monitor. Heroku's own
check is simpler: the dyno boots only if the process binds `$PORT`.

Pushing a container image directly also works if you have Docker locally:

```bash
heroku container:push web --app my-panel
heroku container:release web --app my-panel
```

## Then attach a node

Bots need Docker. Any Linux box works (a Hetzner CX22 is enough to start), and the agent dials out,
so **no inbound ports and no public DNS are needed**:

1. Panel → **Nodes → Add node**, copy the node token.
2. On the server:

```bash
sudo apt-get install -y docker.io util-linux e2fsprogs iptables
curl -fsSL https://raw.githubusercontent.com/jub0t/mechon/main/scripts/install-agent.sh \
  | sudo MECHON_PANEL_URL=https://my-panel.herokuapp.com MECHON_NODE_TOKEN=paste-the-token sh
```

The node shows as **online** within seconds. Users can now sign in, paste a bot token, pick a size
and deploy.

## What is worse on Heroku than on a VPS

- **Deploy artifacts are ephemeral.** Uploaded code lives in `MECHON_DATA_DIR` (`/tmp/mechon-data`).
  Every dyno restart — at least once every 24 hours — wipes it, so existing bots keep running
  (their code is already on the node) but **rollbacks and redeploys of old versions fail** until
  the code is uploaded again. Bot databases on nodes are unaffected.
- **Dyno cycling** also drops the agent WebSocket once a day. Agents reconnect on their own; deploys
  in flight during the cycle can fail.
- **One dyno only.** Postgres-backed sessions survive, but the River job queue is in-process, so
  scaling `web=2` double-runs webhook deliveries.
- **No persistent admin shell.** `heroku run` works for `init` and one-off commands, nothing more.
- **Cost.** Basic dyno ($7/mo) + Postgres Essential-0 ($5/mo) is about $12/mo for a control plane
  that still needs a VPS for the nodes. Running the panel on that same VPS is cheaper.

If you want a single box to run everything, follow [install.md](install.md) instead — it puts the
panel and a node on one server with systemd.

## Why not the Go buildpack

`web/dist` is gitignored (`web/dist/*` with a `.gitkeep` exception), and the Go binary embeds that
directory. A buildpack build would serve the "UI not built" notice unless you commit a build
artifact. The Dockerfile builds the UI with pnpm first, so it is the supported path.
`GO_INSTALL_PACKAGE_SPEC=./cmd/mechon` plus a committed `web/dist` would work, but it is fragile.
