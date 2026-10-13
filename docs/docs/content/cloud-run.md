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

## Scheduled jobs: Cloud Scheduler

Outside Cloud Run, listmonk runs four jobs on timers inside its process. On Cloud Run, an instance has no CPU between requests and may not exist at all, and every instance would run its own timers. With `cloud_run.cron.enabled`, listmonk starts none of these timers, and Cloud Scheduler runs each job by calling `POST /cron/<name>`.

| Setting | Default | |
|---|---|---|
| `cloud_run.cron.enabled` | `false` | Stop the timers and serve `POST /cron/<name>`. |
| `cloud_run.cron.audience` | `""` | The audience of Cloud Scheduler's ID token (`--oidc-token-audience`), eg: the service's URL. Required. |
| `cloud_run.cron.service_account` | `""` | The service account Cloud Scheduler signs in as (`--oidc-service-account-email`). Required. |

The service is public, so listmonk checks the token itself: a request needs a Google-signed ID token for the audience, issued to the service account. Without one, it gets a 401; with another account's, a 403. listmonk does not start when the feature is on and either setting is empty.

| Job | Timer outside Cloud Run | Runs only when the setting is on | Schedule |
|---|---|---|---|
| `slow-query-cache` | `app.cache_slow_queries_interval` (default `0 3 * * *`) | `app.cache_slow_queries` | The interval setting's cron expression |
| `db-vacuum` | `maintenance.db.vacuum_cron_interval` (default `0 2 * * *`) | `maintenance.db.vacuum` | The interval setting's cron expression |
| `update-check` | Every 24 hours | `app.check_updates` | Eg: `0 4 * * *` |
| `token-cleanup` | Every hour | always | `0 * * * *` |

A job whose setting is off answers 200 with `"ran": false` and does nothing, so the Scheduler jobs can stay in place. The Scheduler's schedule replaces the interval settings, which listmonk no longer reads on Cloud Run. Each job is safe to run twice. A job that fails answers 500, and Cloud Scheduler retries it if the Scheduler job has retries.

The update status and the 2FA and password reset tokens are kept in each instance's memory: `update-check` and `token-cleanup` act on the instance that gets the call. Expired tokens are refused whether or not they were cleaned up.

Create a service account for Cloud Scheduler, then one Scheduler job per listmonk job:

```shell
gcloud iam service-accounts create listmonk-scheduler --project PROJECT_ID

gcloud scheduler jobs create http listmonk-db-vacuum --location REGION \
  --schedule "0 2 * * *" --time-zone "Etc/UTC" \
  --uri "https://SERVICE_URL/cron/db-vacuum" --http-method POST \
  --oidc-service-account-email listmonk-scheduler@PROJECT_ID.iam.gserviceaccount.com \
  --oidc-token-audience "https://SERVICE_URL" \
  --attempt-deadline 30m --max-retry-attempts 3 --min-backoff 60s
```

Then set `LISTMONK_cloud_run__cron__enabled=true`, `LISTMONK_cloud_run__cron__audience=https://SERVICE_URL` and `LISTMONK_cloud_run__cron__service_account=listmonk-scheduler@PROJECT_ID.iam.gserviceaccount.com` on the service. `VACUUM` on a large database can run for minutes: keep the attempt deadline and the service's request timeout above it.
