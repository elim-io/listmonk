# Google Cloud Run

listmonk can run on [Google Cloud Run](https://cloud.google.com/run) with a PostgreSQL database on Cloud SQL. The settings below are in the `[cloud_run]` section of the config, or in `LISTMONK_cloud_run__*` environment variables. All of them are off by default, and listmonk behaves as before when they are not set.

## The image

`deploy/cloudrun/Dockerfile` builds an image from the source, with the frontend and the static assets in the binary:

```shell
docker build -f deploy/cloudrun/Dockerfile --build-arg LISTMONK_VERSION=v6.2.0 -t listmonk-cloudrun .
```

The service listens on `$PORT`, which Cloud Run sets. Pass the database and the other settings as `LISTMONK_*` environment variables (secrets from Secret Manager), as with the [Docker image](installation.md).

## Schema install and upgrades: a Cloud Run job

The service does not install or upgrade the schema on start, and listmonk refuses to start while upgrades are pending. Run the same image as a Cloud Run job with the command `/listmonk/migrate.sh` before each deploy. It installs the schema on an empty database (`--install --idempotent`), then runs the pending upgrades (`--upgrade`).

```shell
gcloud run jobs create listmonk-migrate --image IMAGE --region REGION \
  --command /listmonk/migrate.sh --max-retries 0 --task-timeout 30m \
  --set-env-vars ... --set-secrets ...
gcloud run jobs execute listmonk-migrate --region REGION --wait
```

## Health checks

| Setting | Default | |
|---|---|---|
| `cloud_run.ready` | `false` | Serve `GET /ready`, which answers 200 only once Postgres answers, and 503 otherwise. |

Use `/ready` for the startup probe, so that a new instance gets traffic only once it reaches the database, and `/health`, which does not check the database, for the liveness probe. A liveness probe on `/ready` would restart every instance at once during a database outage. Cloud Run reserves some paths ending in `z`, so there is no `/healthz`.

```shell
gcloud run services update listmonk --region REGION \
  --startup-probe=httpGet.path=/ready,periodSeconds=5,failureThreshold=24,timeoutSeconds=3 \
  --liveness-probe=httpGet.path=/health,periodSeconds=10,failureThreshold=3,timeoutSeconds=3
```

## The admin event stream

The admin interface keeps a server-sent event stream open (`GET /api/events`) to show the server's errors as they happen. On Cloud Run, the stream is a request like any other: it ends at the service's request timeout, a reconnect can land on another instance, and an instance only sees its own errors.

| Setting | Default | |
|---|---|---|
| `cloud_run.events_relay` | `false` | Pass the stream's messages between instances through Postgres `LISTEN`/`NOTIFY` (channel `listmonk_events`), so that every browser sees the errors of every instance. Each instance keeps one extra database connection. |
| `cloud_run.events_max_age` | `"0"` | End each stream after this long, eg: `"10m"`. The browser reconnects 3 seconds later. `"0"` is no limit. |
| `cloud_run.events_ping` | `"0"` | Write a keep-alive comment this often, eg: `"25s"`. `"0"` sends none. |

Set the service's request timeout above `events_max_age`, at most 60 minutes:

```shell
gcloud run services update listmonk --region REGION --timeout 3600
```

An open stream is a request in progress, so an instance is billed while an admin page is open. Messages sent while a browser is reconnecting are not sent again.
