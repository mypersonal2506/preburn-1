# Self-hosting

This page covers running Preburn with the `compose.yaml` of the [quickstart](../README.md#quickstart) on a server: TLS, the secret key, backups, upgrades, scaling, sizing and monitoring. Run every command in the folder that holds `compose.yaml` and `.env`.

## What runs

| Service | Image | Role |
|---|---|---|
| `postgres` | `postgres:18.6-alpine` | Stores every record. Volume `postgres`, port not published. |
| `valkey` | `valkey/valkey:9.1.2-alpine` | Holds the counters, reservations and the decision stream, with append-only persistence. Volume `valkey`, port not published. |
| `migrate` | `ghcr.io/preburn/preburn` | Runs `preburn migrate` once at each start: applies migrations and imports the pricing catalog, then exits. |
| `api` | `ghcr.io/preburn/preburn` | Runs `preburn serve`: the API and the dashboard on `127.0.0.1:${PREBURN_PORT}`. |
| `worker` | `ghcr.io/preburn/preburn` | Runs `preburn worker`: expiry, counter repair, rollups, usage estimates, retention and cleanup jobs. |

`api` and `worker` start after `migrate` succeeds and restart unless stopped. Their Compose healthchecks call `/readyz` on the api and the metrics listener on the worker. The Compose project is named `preburn`, so the volumes are `preburn_postgres` and `preburn_valkey`. The name does not follow the folder, so to run a second installation on the same machine, pass `-p <name>` to every `docker compose` command in its folder and set another `PREBURN_PORT` in its `.env`.

`docker compose down` stops Preburn and keeps its data. `docker compose down -v` also deletes the volumes and every record in them.

## Secret key

`PREBURN_SECRET_KEY` encrypts the secrets Preburn stores, such as the Stripe keys and webhook signing secrets of later releases. Version 0.1 stores none yet, but every process requires the key.

- Create it once with `docker run --rm ghcr.io/preburn/preburn secret-key` and keep it in `.env`. Restrict the file with `chmod 600 .env`.
- Back it up apart from the database dumps. A dump without its key cannot decrypt the secrets inside it.
- Do not change it. Secrets encrypted with the old key become unreadable.

## TLS and a reverse proxy

The api serves plain HTTP on `127.0.0.1`. To reach it from other machines, put a reverse proxy with TLS on the same host.

With [Caddy](https://caddyserver.com), which obtains certificates on its own, the whole `Caddyfile` is:

```
preburn.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

With nginx:

```nginx
server {
    listen 443 ssl;
    server_name preburn.example.com;
    ssl_certificate /etc/letsencrypt/live/preburn.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/preburn.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

The decision stream sends `X-Accel-Buffering: no` and a heartbeat every 15 seconds, so it passes through nginx's default buffering and timeouts. nginx's default body limit of 1 MiB matches Preburn's.

Then tell Preburn about the proxy in `.env`:

```sh
PREBURN_PUBLIC_URL=https://preburn.example.com
PREBURN_TRUSTED_PROXIES=172.18.0.1/32
```

- `PREBURN_PUBLIC_URL` sets the address of setup, invite and reset links, and its `https` scheme marks the session cookies `Secure`.
- `PREBURN_TRUSTED_PROXIES` lists the addresses the proxy's connections arrive from. Requests reaching the published port arrive from the gateway of the Compose network, which this command prints:

  ```sh
  docker network inspect preburn_default --format '{{range .IPAM.Config}}{{.Gateway}}{{end}}'
  ```

  Without it, every visitor shares the proxy's address, and the sign-in rate limit of 10 attempts per 15 minutes per address applies to all of them together.

Apply changes to `.env` with `docker compose up -d --wait`.

## Backups

Postgres holds everything worth keeping. Valkey holds counters that Preburn rebuilds from Postgres. Back up the database with `pg_dump`:

```sh
docker compose exec -T postgres pg_dump --username=preburn --format=custom preburn > preburn-$(date +%F).dump
```

Keep the dumps and `.env`, which holds the secret key, off the server.

### Restore

A dump always goes into a new, empty database. `pg_restore --clean` over the live database drops only the objects in the dump, so tables that a newer release added would stay behind and break the next upgrade.

1. On a new server, start the stack as in the quickstart with the same `.env`.
2. Stop the processes that use the database, keep the current database under another name and create an empty one:

   ```sh
   docker compose stop api worker
   docker compose exec -T postgres psql --username=preburn --dbname=postgres --command='ALTER DATABASE preburn RENAME TO preburn_before_restore'
   docker compose exec -T postgres createdb --username=preburn preburn
   ```

3. Restore the dump, empty Valkey and start everything again:

   ```sh
   docker compose exec -T postgres pg_restore --username=preburn --dbname=preburn --exit-on-error < preburn-2026-09-27.dump
   docker compose exec valkey valkey-cli FLUSHALL
   docker compose up -d --wait
   ```

Emptying Valkey removes counters and reservations that the restored database no longer matches, and the api rebuilds them from Postgres before `/readyz` answers 200. Once Preburn works on the restored data, remove the database you set aside:

```sh
docker compose exec postgres dropdb --username=preburn preburn_before_restore
```

## Upgrades

1. Read the [changelog](../CHANGELOG.md). Preburn is at 0.x, so a minor release can change the API and the configuration.
2. Take a backup.
3. Set the new version in `.env`, such as `PREBURN_VERSION=0.2.0`.
4. Pull and restart:

   ```sh
   docker compose pull
   docker compose up -d --wait
   ```

`migrate` applies the new migrations and imports the release's pricing catalog before `api` and `worker` start again. While the api restarts, the SDK answers checks with its fallback decisions. Migrations only go forward. To go back to the earlier version, follow [Restore](#restore) with the backup you took before the upgrade, and set `PREBURN_VERSION` back to the earlier version in `.env` before the last `docker compose up -d --wait`.

## Scaling

- Run more workers with `docker compose up -d --scale worker=2 --wait`. Periodic jobs run on one elected worker, and the other jobs spread across all of them.
- Api processes keep their state in Postgres and Valkey. Their in-process caches of keys, customers, policies and prices follow the changes every process announces through Valkey, so any number of api replicas can serve behind a load balancer. `compose.yaml` publishes one fixed port for one api container, so running more replicas needs a load balancer in front of them in place of that port. Give the load balancer an idle timeout above 15 seconds for the decision stream.
- Each process opens up to `PREBURN_DATABASE_MAXIMUM_CONNECTIONS` Postgres connections, 10 by default, plus one while it holds a lock for migrations or a counter rebuild. The bundled Postgres allows 100.
- Postgres and Valkey run as single instances. To use managed ones, see [Compose variables](configuration.md#compose-variables).

## Sizing

Measured with the v0.1 image on a 4 CPU, 8 GiB virtual machine that ran every service and the load generator:

- After the quickstart, the api used about 98 MiB of memory, the worker 14 MiB, Postgres 62 MiB and Valkey 4 MiB. The database was 15 MB, most of it the pricing catalog.
- `preburn bench check` for 60 seconds with 20 requests in flight over 100 customers sent 117,585 checks and as many reports, about 1,960 of each per second, with no errors.

| Operation | p50 | p95 | p99 |
|---|---|---|---|
| check | 3.6 ms | 10.1 ms | 20.2 ms |
| report | 4.0 ms | 12.5 ms | 32.6 ms |

- Storage with indexes was about 1.6 kB per decision and 0.8 kB per ledger entry. Decisions are deleted after the retention period, 90 days by default, and ledger entries are kept. At 100,000 calls a day that is about 14 GB of decisions at any time and 29 GB more ledger entries each year.

To measure your own server, run the benchmark with a runtime key of the test environment:

```sh
docker compose exec api /preburn bench check --api-url http://127.0.0.1:8080 --api-key pb_test_runtime_...
```

It creates the customers `bench-customer-0` to `bench-customer-99` in the test environment and leaves its decisions and ledger entries there.

## Monitoring

### Health

`GET /healthz` answers 200 while the api process runs. `GET /readyz` answers 200 when Postgres, Valkey, the Lua scripts and the counters are ready, and 503 with the failing checks otherwise.

### Logs

Every process writes JSON lines to standard output, which `docker compose logs` shows. Each line's `msg` is an event name, such as `http.request_completed` or `decisions.settle_deferred`, and request lines carry `request_id`. Preburn never logs passwords, tokens, API keys or secrets.

### Metrics

The api and the worker serve Prometheus metrics on port 9090 inside the Compose network, not published on the host. Scrape `api:9090` and `worker:9090` from a Prometheus that joins the network.

| Metric | Meaning |
|---|---|
| `preburn_http_requests_total` | HTTP requests by route pattern, method and status. |
| `preburn_http_request_duration_seconds` | HTTP request duration by route pattern and method. |
| `preburn_check_duration_seconds` | Duration of checks that decided, by outcome. |
| `preburn_decisions_total` | Decisions by environment, outcome and reason. |
| `preburn_reports_total` | Usage reports by environment, cost status and duplicate. |
| `preburn_dropped_reports_total` | Reports the SDK dropped, by environment. |
| `preburn_reservations_expired_total` | Reservations released because their hold time passed. |
| `preburn_counter_repairs_total` | Counters the repair job changed because they differed from Postgres. |
| `preburn_counters_rebuild_duration_seconds` | Duration of rebuilding the counters from Postgres. |
| `preburn_jobs_completed_total` | Finished job attempts by kind and outcome. |
| `preburn_river_queue_available` | Jobs waiting to run, by queue. |

### Traces

Set `OTEL_EXPORTER_OTLP_ENDPOINT` to export OpenTelemetry traces. See [Tracing variables](configuration.md#tracing-variables).

## Access recovery

A member who lost their password gets a new link from another member on the dashboard's Members page, or from the server:

```sh
docker compose exec api /preburn admin reset-password --email you@example.com
```

The command prints a reset link that works once within 24 hours. `preburn admin create` adds a member with a password from the terminal.
