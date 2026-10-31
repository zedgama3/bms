# Telemetry

This page has two parts. The first is for the people who use a regional's bms: what the site records about its use and what it never records. A regional can hand it to its participants or paste it into its privacy notice. The second is for the operator: how to send bms's logs, traces and metrics to an OpenTelemetry collector.

How to run bms is in [operating.md](operating.md).

## For participants: what is recorded

bms records two kinds of information about how the site is used. Neither is advertising or tracking, and nothing is sent to a third party unless the regional's operator has set up its own collector.

### 1. When something breaks (error reports)

When a screen crashes, a request fails in your browser, or a part of the site cannot load, your browser sends a short report to the site's own server. It is turned into a log line so a maintainer can find and fix the problem.

A report may contain:

- what kind of problem it was (a crash, a failed request, a part of the page that did not load);
- the message and technical trace of the error, with e-mail-like text and anything after a `?` in an address removed, and cut to a limited length;
- which screen you were on (with record numbers replaced);
- for a failed request, which kind of request failed, how long it took, and the error reference shown on your screen;
- your browser's name and version string (cut to 200 bytes);
- **if you are signed in, your account's ID.** This is an opaque number, not your name. It exists so a maintainer can help with the error you saw. A maintainer can look up whose number it is, but only with the permission to do so.

Error reports are limited in number per address, so one broken page cannot flood the service. Your network address is used only to apply that limit, is held in memory for at most a minute of inactivity, and bms never logs or exports it. The front end's own logs are the operator's: see [Front-end logs are personal data](operating.md#front-end-logs-are-personal-data); the example nginx configuration leaves addresses out of its access log.

Error reports have no off switch: they are how the service is kept working. The project recommends keeping log lines for 30 days; your regional's operator sets the retention.

### 2. How the site is used (usage counts)

The site counts which screens are used and how fast they are, so the regional knows which parts people use and which are slow, especially on phones.

An event contains only words and numbers from a fixed list built into the site:

- the screen's name (a template such as "shift list", never a link with a person or record in it);
- for a screen change, which screen you came from;
- the name of an action that screen has declared (for example "export started");
- how long the screen took to be ready and standard page-speed numbers (Web Vitals: loading, responsiveness, layout shift);
- the kind of device: phone-sized or larger, and the browser family, worked out by the server from your browser's identifier (the identifier itself is not kept).

These become **counts and timing buckets only**. They are not stored per person and not per visit. The site never records, for usage counts: your account ID, a session or visit identifier, your name, e-mail address, a link or query string, anything you typed or chose, a record's ID, or a trace identifier. It cannot tell whether two events came from the same person. There is no list of "return visits" or "journeys".

The project recommends keeping counts in aggregate for one year; your regional's operator sets the retention.

**Your choices.** Your browser can switch usage counting off: if it sends **Global Privacy Control** or **Do Not Track**, the site does not start the counting at all and sends nothing. The regional can also switch usage counting off for everyone (see below). Error reports (part 1) are not affected by either.

### What is never recorded

Not in logs, traces, metrics or error reports: names (legal or playa), e-mail addresses, phone numbers, postal addresses, emergency contacts, incident or medical text, anything typed into a form, request or response bodies, query strings, database statements or their values, session tokens, passwords, or the database URL. The site removes such things before anything leaves it, and it counts any attempt as a bug.

Personal data about you that bms holds (your profile, shifts, registrations) is covered by the regional's own privacy notice and by the export and deletion tools. Logs and traces hold your account's ID only, cannot be searched by person inside bms, and expire with the retention above, so they are outside the per-person export and deletion tools.

## For operators

### What bms exports

bms writes logs to standard output (JSON by default, with the trace ID on every record in a request). It can also export all three signals over OTLP:

- **traces**: one span per request and for database statements, jobs, mail, and file operations;
- **metrics**: request rates and durations, errors by route and code, the database pool, jobs, the mail outbox, file operations, client error counts, and the usage counts above;
- **logs**: the same log records as standard output.

Each error shown to a person carries a reference like `1A2B-3C4D`, derived from the trace ID. A person can quote it; you can find the request from it.

If no collector is configured, nothing is exported. Standard output (the journal) still has the logs.

### Pointing bms at a collector

bms speaks **OTLP over HTTP with protobuf only** (`http/protobuf`). It does not speak gRPC and does not read `OTEL_TRACES_EXPORTER` and the like: if an endpoint is set it exports with OTLP/HTTP, otherwise it exports nothing. A collector that accepts OTLP/HTTP on port 4318 (the OpenTelemetry Collector, Grafana Alloy and many others) works.

bms drops idle connections to the collector after 30 s, so a proxy or load balancer in front of the collector must keep idle connections open longer than 30 s; otherwise exports can fail intermittently with `EOF`.

Set these in the environment (for the systemd install, `/etc/bms/bms.env`; the commented lines are already in `bms.env.example`) and restart:

```
OTEL_EXPORTER_OTLP_ENDPOINT=https://otel.example.org:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer%20CHANGE-ME
OTEL_SERVICE_NAME=bms
OTEL_RESOURCE_ATTRIBUTES=deployment.environment=production
```

The variables bms checks itself at start (a bad value stops it, exit 2, naming the variable):

| Variable | Meaning |
|---|---|
| `OTEL_SDK_DISABLED` | `true` turns every exporter off (logs still go to standard output). |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Base URL. Each signal appends `/v1/traces`, `/v1/metrics`, `/v1/logs`. An absolute `http` or `https` URL. |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, `_METRICS_ENDPOINT`, `_LOGS_ENDPOINT` | A full URL for one signal; wins over the base for that signal. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` and the three per-signal variants | If set, must be `http/protobuf`. |
| `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG` | `always_on` (the default), `always_off`, or `traceidratio` with an argument from 0 to 1. |
| `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES` | Names this service in the collector. |

Read by the OpenTelemetry library itself, with its own defaults: `OTEL_EXPORTER_OTLP_HEADERS` (and per-signal), `OTEL_EXPORTER_OTLP_TIMEOUT`, `OTEL_EXPORTER_OTLP_CERTIFICATE`, `_CLIENT_CERTIFICATE`, `_CLIENT_KEY`, `_COMPRESSION`, `OTEL_BSP_*`, `OTEL_BLRP_*`, `OTEL_METRIC_EXPORT_INTERVAL`, `OTEL_METRIC_EXPORT_TIMEOUT`, `OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT` and `OTEL_SPAN_ATTRIBUTE_COUNT_LIMIT`.

A signal is exported only if `OTEL_SDK_DISABLED` is not `true` and the signal has an endpoint.

**`OTEL_EXPORTER_OTLP_HEADERS` is a credential** (it usually carries a token). bms never logs it and `bms check` never prints it. Keep `bms.env` mode 0640 and out of backups others can read. `bms check` prints `OTLP export: on` or `off` (no list follows).

### Turning usage counting off

Set `BMS_TELEMETRY_USAGE=false` and restart. It is on by default because the events hold nothing about a person or visit by construction. With it off, the usage route answers "not supported" (501), the site stops sending events for that page load, and the rejected events are counted as `disabled`. Error reports (part 1) are unaffected. `bms check` prints `usage counts: on (...)` or `off`.

### Retention

Retention is set by the collector and the backends, not by bms. The project's chosen defaults, as set up in the development stack in `deploy/compose/observability`:

| Data | Default | Where it is set |
|---|---|---|
| Logs | 30 days | The log store (Loki). Not yet set in the development stack's configuration: not yet decided how it ships. |
| Traces | 30 days | Tempo (`block_retention: 720h`). |
| Metrics | 1 year | Prometheus (`--storage.tsdb.retention.time=1y`). |
| Standard output on the host | the host's | journald or your log rotation. Rotating it is the operator's job. |

Logs and traces hold account IDs, so they should expire. **Grafana (or whatever you view telemetry in) is for maintainers only.** Do not give it to ordinary volunteers.

### Finding a request from an error reference

In Grafana: Explore, Loki, then

```
{service_name="bms"} | json | error_ref="1A2B-3C4D"
```

The record carries `trace_id`; open the trace in Tempo, and Tempo's "logs for this trace" shows every record of the request. On a terminal with text logs: `grep 1A2B-3C4D`.

### The development stack

For developers, `make observe` (see the `Makefile`) starts Alloy, Loki, Tempo, Prometheus and Grafana from `deploy/compose` with the `observability` profile, and Alloy listens for OTLP on port 4318. To point a local bms at it set `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318`. It is for development, not a production recommendation. A production example of collector and backends is not yet written.

### Dashboards and alerts

The design lists six Grafana dashboards (overview, API, client, usage, database, jobs and mail) and fourteen alert rules (server error rate, slow requests, client error spikes, chunk-load failures after a deploy, poor Web Vitals, API errors by route, job failures, mail delivery failing, database pool saturated, slow queries, telemetry export failing, intake rate limited, privacy-policy violations, and no traffic). The shipped example thresholds are written as commented constants for the operator to tune.

**Where they are.** The dashboards are in `deploy/observability/grafana/dashboards/` and the rules in `deploy/observability/alerts/bms.rules.yaml`; `deploy/observability/README.md` says how to install them in your own Grafana and Prometheus or Mimir. The dashboards pick their data sources through variables, so they work whatever your data sources are called. The rules decide when something is wrong; where a firing alert is sent (e-mail, a chat channel) is set in your Alertmanager or Grafana, not here. On another backend, re-create them from the metric names, which appear as Prometheus renders them: for example `http_server_request_duration_seconds_bucket`, `bms_client_errors_accepted_total`, `bms_client_screen_views_total`, `bms_api_errors_total`, `bms_jobs_runs_total`, `bms_notify_delivered_total`, `bms_telemetry_export_failures_total` and `bms_telemetry_policy_violations_total`. The last one should never rise: a rise means a privacy bug reached production, so it pages.

The usage counter `folded` rising means the screens the site was built with and the usage route's list disagree.
