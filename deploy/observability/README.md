# Observability assets

Grafana dashboards and Prometheus alert rules for bms (telemetry.md, T1.8). They are shared:
the development compose stack uses them as they are, and any other Grafana, Prometheus or
Mimir installation can install the same files.

| Path | What it is |
|---|---|
| `grafana/dashboards/*.json` | Six dashboards, tagged `bms`, linked to each other: `bms-overview`, `bms-api`, `bms-client`, `bms-usage`, `bms-database`, `bms-jobs-notify` |
| `grafana/provisioning/dashboards.yaml` | Grafana file provider that loads them into a `bms` folder |
| `alerts/bms.rules.yaml` | Fourteen alert rules in Prometheus rule format, one group `bms`, labelled `severity` = `page`, `warn` or `info` |

Every dashboard has an `instance` variable (the bms copy, from `target_info`) and three
data-source variables, `prometheus`, `loki` and `tempo`. Panels refer to `${prometheus}` and
so on, never to a fixed UID, so the dashboards work with whatever data sources an
installation provisions. The client and usage dashboards add `module` and `screen`; the
jobs dashboard adds `module`.

Metric names are as Prometheus renders the OpenTelemetry instruments
(`http_server_request_duration_seconds_bucket`, `bms_client_screen_views_total`). Logs are
read from Loki 3 with OTLP ingestion (`{service_name="bms"}`, with `origin`, `client_kind`
and the other attributes as structured metadata). The database dashboard and the
`BmsSlowQueries` rule need Tempo's span metrics (`traces_spanmetrics_*`), which carry
`service` but not `instance`.

Every rule's threshold is an example value, marked `THRESHOLD` in a comment above it. Tune
them for your installation.

## In the compose stack

`make observe` starts Grafana, Loki, Tempo, Prometheus and Alloy
(`deploy/compose/docker-compose.yml`, profile `observability`). It mounts:

- `grafana/provisioning/dashboards.yaml` as `/etc/grafana/provisioning/dashboards/bms.yaml`,
  and `grafana/dashboards/` as `/etc/grafana/dashboards/bms`;
- `alerts/` as `/etc/prometheus/rules`, loaded by `rule_files` in
  `deploy/compose/observability/prometheus.yml`. There is no Alertmanager: firing alerts show
  on Prometheus's Alerts page.

Retention in the stack is 30 days for logs (`deploy/compose/observability/loki.yaml`), 30
days for traces (`tempo.yaml`) and 1 year for metrics (Prometheus's flag in the compose file).

To check that the six dashboards were provisioned, once Grafana is up:

```sh
curl -s 'http://localhost:3000/api/search?tag=bms' | grep -o '"uid":"bms-[a-z-]*"'
```

It prints six UIDs. Dashboard edits made in the Grafana UI are not saved back
(`allowUiUpdates: false`): export the JSON and replace the file.

## In another Grafana and Prometheus or Mimir

Dashboards:

1. Copy `grafana/dashboards/*.json` to a directory Grafana can read.
2. Copy `grafana/provisioning/dashboards.yaml` into Grafana's `provisioning/dashboards/`
   and set `options.path` to that directory.
3. Restart Grafana, or wait for `updateIntervalSeconds`. Pick the data sources in the
   variables at the top of each dashboard if the defaults are not the right ones.

You can also import a file by hand (Dashboards, New, Import); the provider is then not needed.

Rules, for Mimir's ruler:

```sh
mimirtool rules load --address=<mimir-url> --id=<tenant> alerts/bms.rules.yaml
```

For Prometheus, list the file under `rule_files` in `prometheus.yml` and reload. Route the
`severity` label to receivers in Alertmanager.
