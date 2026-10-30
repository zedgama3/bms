# Operating bms

This page is for the volunteer who runs a regional's copy of bms. It is plain on purpose: you do not need to be a programmer, but you do need to be comfortable with a Linux command line.

Examples use `example.org` for addresses. Replace it with your own. Where the design has not settled something, this page says "not yet decided" rather than guessing.

## What bms needs

- **A Linux server** with systemd. bms is one program (`bms`) with the web interface built in; no Node or other runtime is needed on the server. Which distributions are supported is not yet decided.
- **PostgreSQL 16 or newer.** bms keeps everything about people, shifts and incidents here. bms refuses to start against an older server.
- **A front end that ends TLS** (HTTPS), such as nginx, Caddy or a tunnel. bms itself speaks plain HTTP on a loopback address and trusts the front end for HTTPS. Never expose bms's own port to the internet.
- **A modern browser** for everyone who uses it: browsers from the last two and a half years. The interface is built phone first. There is no "update your browser" page.
- **Optional:** an SMTP relay for e-mail (password resets, notifications), an S3-compatible store for uploaded files, an OpenTelemetry collector (see [telemetry.md](telemetry.md)).

Everything the core needs is open-source software. Anything else is an optional module.

## Install with systemd

The files referred to here are in the `deploy/` folder of the source: `deploy/systemd/bms.service`, `deploy/systemd/bms.env.example`, `deploy/nginx/bms.conf`. How you get the `bms` binary (a release download or building it yourself) is not yet decided; this page starts from a binary you already have.

### 1. User, directories and binary

```sh
sudo useradd --system --home-dir /var/lib/bms --shell /usr/sbin/nologin bms
sudo install -d -o bms -g bms -m 0700 /var/lib/bms
sudo install -d -o root -g bms -m 0750 /etc/bms
sudo install -m 0755 bms /usr/local/bin/bms
```

`/var/lib/bms` is bms's data directory (`BMS_DATA_DIR`). It holds the secrets file, the uploaded files (under `files/`) and the audit spool (under `audit/spool/`). Treat it like the database: back it up and keep it private.

### 2. The database and its role

Run as the PostgreSQL superuser (for example `sudo -u postgres psql`):

```sql
CREATE ROLE bms LOGIN PASSWORD 'choose-a-long-random-password';
CREATE DATABASE bms OWNER bms;
```

The `bms` role must be able to create schemas in its database and own what it creates; owning the database does that. bms makes one schema per module and needs nothing outside its own database. Do not make the role a PostgreSQL superuser.

To pin TLS to a remote database, put `?sslmode=verify-full` in the URL, or set `PGSSLMODE` and `PGSSLROOTCERT`. The example URL uses `sslmode=disable`, which is only right when PostgreSQL is on the same host.

### 3. The environment file

```sh
sudo install -m 0640 -o root -g bms deploy/systemd/bms.env.example /etc/bms/bms.env
sudoedit /etc/bms/bms.env
```

Set at least `BMS_DB_URL` (with the real password). The example also sets the data directory, loopback listening address, and `BMS_HTTP_TRUSTED_PROXIES=127.0.0.1/32`, which is right for a front end on the same host. The file holds the database password: keep it out of backups other people can read. The tighter alternative is `BMS_DB_URL_FILE`: name a file (mode 0600, owned by `bms`) that holds the URL, so the password is not in the environment file at all. The unit can write only to `/var/lib/bms`: if `BMS_FILES_DIR` or `BMS_SECRETS_FILE` points elsewhere, add that path to `ReadWritePaths=` with a drop-in (`systemctl edit bms`), or bms cannot write there.

Every key is in the [configuration catalogue](#configuration-catalogue) below.

### 4. Create the schema and the secrets file

`bms migrate` must run once before the first start, as the `bms` user with the same environment. One way that needs no shell tricks:

```sh
sudo systemd-run --pipe --wait --collect -p User=bms -p Group=bms \
  -p EnvironmentFile=/etc/bms/bms.env -p WorkingDirectory=/var/lib/bms -p UMask=0077 \
  /usr/local/bin/bms migrate
```

(Written from the unit file's settings; not yet tried on every distribution.) `migrate` creates the secrets file (`/var/lib/bms/secrets.json`, mode 0600) when it does not exist, then applies the database migrations in order. It prints a notice about the secrets file once. See [the secrets file](#the-secrets-file).

### 5. Check, then start

Run `bms check` the same way (replace `migrate` with `check` above). It prints the configuration it read (secrets are never shown), the modules, the database and the schema, and exits the way `serve` would. Fix anything it reports before starting.

```sh
sudo install -m 0644 deploy/systemd/bms.service /etc/systemd/system/bms.service
sudo systemctl daemon-reload
sudo systemctl enable --now bms
sudo journalctl -u bms -f
```

If PostgreSQL runs on the same host under a unit name other than `postgresql.service`, add it to `After=` with a drop-in (`systemctl edit bms`). The unit asks systemd to stop bms with SIGTERM and waits 30 seconds; this is longer than the 15 seconds bms gives its whole shutdown (`BMS_HTTP_SHUTDOWN_TIMEOUT`); draining in-flight HTTP requests gets about two-thirds of that, and the rest goes to the other shutdown steps. If you raise that setting, raise `TimeoutStopSec` above it.

`bms serve` logs to standard output (the journal). Other commands log to standard error. A build without the web interface serves the API only. Rotating logs is the host's job (journald does it by default).

### 6. nginx (or another front end)

`deploy/nginx/bms.conf` is an example for a front end on the same host. What any front end must do:

- Keep the `Host` header (`proxy_set_header Host $host`). bms has no `Host` allowlist; it uses `Host` for the Origin-versus-Host check that guards against cross-site requests (api decision 6), so a changed `Host` makes that check fail. Until a public-URL setting exists, the front end's virtual host is the allowlist: give it a `server_name`, and do not make bms's server the catch-all for names that are not yours.
- Send `X-Forwarded-For` and `X-Forwarded-Proto`, replacing (never appending to) any the client sent. Behind a tunnel, take the visitor address from the tunnel's header with nginx's `real_ip` module, as `deploy/nginx/bms.conf` does. bms believes them only from the addresses in `BMS_HTTP_TRUSTED_PROXIES`.
- Do no authentication of its own.
- Allow uploads up to bms's own limit. The default is 25 MiB (`files.max_bytes`, a setting); the example allows 30 MiB.
- Terminate TLS. The example listens on loopback port 80 for use behind a tunnel or load balancer that ends TLS first; for a public server, listen on 443 with your certificate and, when nginx itself ends TLS, send `$scheme` as `X-Forwarded-Proto`.

A front end on the same host is listed as `BMS_HTTP_TRUSTED_PROXIES=127.0.0.1/32`. Otherwise every visitor looks like loopback and sign-in rate limits and HTTPS-only headers go wrong. A front end on another machine or a compose network: bind bms to `:8080` and list that machine's address or the network's prefix. An entry that trusts everything (zero prefix bits) is refused.

bms sends `Strict-Transport-Security: max-age=31536000` only. If you control every subdomain of your domain and want `includeSubDomains` or `preload`, add that at the front end.

Health checks: `/healthz` (the process is up) and `/readyz` (the database answers). Probes should come from loopback or through the front end.

### Front-end logs are personal data

The front end sees every request, so its logs are personal data even though bms keeps its own clean. The access log of `deploy/nginx/bms.conf` (`bms_private`) has no visitor addresses, no query strings (`$uri`, not the request line) and no `Referer`. The reason: a password-reset link carries its one-time token in the query string, a search carries a person's name, and the `Referer` repeats both.

nginx's error log can still hold an address and a request line when something goes wrong. Restrict who can read it (mode 0640, a group of operators) and rotate it, deleting old files. Do the same for any other log the front end or tunnel writes.

## Configuration catalogue

bms reads its configuration from `BMS_*` environment variables only. `bms check` prints what it read. An unknown `BMS_*` variable is reported as a problem, which catches typos. Values with a trailing `_FILE` (for keys marked "secret") name a file holding the value instead.

| Variable | Default | Meaning |
|---|---|---|
| `BMS_INSTANCE_NAME` | `main` | Name of this copy, recorded by audit and telemetry. 1 to 64 characters of lower-case letters, digits and `-`. Give each copy (writer, on-site copy) its own. |
| `BMS_DB_URL` (secret; `_FILE`) | required | PostgreSQL URL (`postgres://` or `postgresql://`). |
| `BMS_DB_MAX_CONNS` | `10` | Largest database connection pool, 1 to 1000. |
| `BMS_DB_CONNECT_TIMEOUT` | `30s` | How long start-up may take to reach the database, 1s to 10m. |
| `BMS_DB_QUERY_TIMEOUT` | `10s` | Default deadline of one statement, 100ms to 10m. |
| `BMS_HTTP_ADDR` | `127.0.0.1:8080` | Listen address (`host:port` or `:port`). A non-loopback host needs trusted proxies or `BMS_HTTP_ALLOW_PLAINTEXT`. |
| `BMS_HTTP_REQUEST_TIMEOUT` | `30s` | Deadline of one HTTP request, 1s to 10m. |
| `BMS_HTTP_SHUTDOWN_TIMEOUT` | `15s` | How long in-flight requests get to finish at stop, 1s to 5m. |
| `BMS_HTTP_MAX_BODY_BYTES` | `1048576` | Largest request body in bytes, 1024 to 1073741824 (uploads have their own limit). |
| `BMS_HTTP_TRUSTED_PROXIES` | empty | Comma-separated addresses or CIDR networks whose `X-Forwarded-*` headers are believed. Up to 64; never a zero-bit prefix. |
| `BMS_HTTP_ALLOW_PLAINTEXT` | `false` | Permit a non-loopback bind with no trusted proxy. Development only. |
| `BMS_SESSION_KEY` (secret; `_FILE`) | generated | Key that signs sign-in cookies, 32 to 1024 bytes. Normally left unset: `bms migrate` generates it into the secrets file. Set in one place only. |
| `BMS_SETTINGS_SEALING_KEY` (secret; `_FILE`) | generated | Key that seals secret settings (the SMTP password), 32 to 1024 bytes. Same rule as the session key. |
| `BMS_SECRETS_FILE` | `<BMS_DATA_DIR>/secrets.json` | Path of the generated-secrets file. If it exists it must be a regular file, owner-only (0600), at most 64 KiB. |
| `BMS_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. `debug` may log personal data: `bms check` and start-up warn about it. |
| `BMS_LOG_FORMAT` | `json` | `json` or `text`. Use `json` in production. |
| `BMS_DATA_DIR` | `data` | Directory for local files, including the secrets file and the audit spool. Use an absolute path. Must be writable. |
| `BMS_FILES_STORE` | `disk` | Where file contents live: `disk` or `s3`. |
| `BMS_FILES_DIR` | `<BMS_DATA_DIR>/files` | Uploads in flight (`tmp/`) and, for `disk`, the stored files (`store/`). |
| `BMS_FILES_S3_ENDPOINT` | empty | S3 host[:port] or `http(s)://host[:port]`. No scheme means `https`, except a loopback host (`http`). Required for `s3`. |
| `BMS_FILES_S3_BUCKET` | empty | The one private bucket that holds every file. Required for `s3`. |
| `BMS_FILES_S3_REGION` | `us-east-1` | S3 region; self-hosted stores accept the default. |
| `BMS_FILES_S3_PREFIX` | empty | Optional key prefix inside the bucket, such as `bms/`. |
| `BMS_FILES_S3_PATH_STYLE` | `true` | Address the bucket in the URL path (every self-hosted store). |
| `BMS_FILES_S3_ACCESS_KEY` (secret; `_FILE`) | empty | S3 access key. Required for `s3`. |
| `BMS_FILES_S3_SECRET_KEY` (secret; `_FILE`) | empty | S3 secret key. Required for `s3`. |
| `BMS_DEMO_ENABLED` | `false` | Run as the public demo. Exactly `true` or `false`. See [Demo mode](#demo-mode). |
| `BMS_DEMO_PUBLIC_URL` | empty | The demo's canonical public URL (`http` or `https`, at most 256 bytes). Requires `BMS_DEMO_ENABLED=true`. |
| `BMS_TELEMETRY_USAGE` | `true` | Count screen views, declared actions and Web Vitals by screen, never by person. Exactly `true` or `false`. See [telemetry.md](telemetry.md). |
| `BMS_MODULES_DISABLED` | empty | Comma-separated names of compiled-in modules not to run. A name that is not a module is an error. |

Not in the table:

- `OTEL_*` variables are read by the telemetry code; see [telemetry.md](telemetry.md).
- `PG*` variables and `~/.pgpass` are honoured by the database driver for anything the URL leaves unset.
- `BMS_TEST_*` variables are read only by the project's own tests. Never set them in production.

Regional choices that are not about the process (the regional's name, language and public URL, sign-in rules, mail, upload limits, retention) are **settings**, changed by a settings administrator on the settings screen without a restart. They are listed where they matter below.

## Commands and exit statuses

| Command | What it does |
|---|---|
| `bms serve` | Run the server until SIGINT or SIGTERM. |
| `bms migrate` | Create the secrets file on a new installation and apply migrations. Writing copy only. |
| `bms check` | Report configuration, modules, database and schema; exit as `serve` would. |
| `bms create-superuser <email>` | Create or reset the superuser; the password is shown once. |
| `bms export <person-id>` | Print everything held about one person as JSON (personal data: handle it so). |
| `bms delete <person-id>` | Delete one person everywhere and print what was deleted. |
| `bms files verify` | Check the stored files against their records. |
| `bms demo seed` / `bms demo reset` | Demo mode only. |
| `bms openapi` | Print the API description (needs no configuration). |
| `bms version`, `bms help` | Version, usage. |

Exit statuses (scripts and systemd act on them):

- `0`: fine (also a `serve` stopped by a signal).
- `1`: a runtime failure.
- `2`: the configuration is wrong (a bad variable, an unknown module name, no session key) or the command was refused (for example a demo command on a non-demo instance).
- `3`: the database schema is behind or ahead of this binary, a migration file changed, or another migration holds the lock.

Run `bms check` before `bms serve`. One SIGTERM stops `serve` in an orderly way; a second signal ends it at once. SIGHUP does nothing: configuration is not reloaded, restart to change it.

Run these as the `bms` user with the environment file, in the same `systemd-run` form as `migrate`. Keep `--pipe --wait`: without `--pipe`, the superuser password (shown once) or a person's export goes to the journal, where it stays and is readable by others.

```sh
run_bms() {
  sudo systemd-run --pipe --wait --collect -p User=bms -p Group=bms \
    -p EnvironmentFile=/etc/bms/bms.env -p WorkingDirectory=/var/lib/bms -p UMask=0077 \
    /usr/local/bin/bms "$@"
}
run_bms create-superuser admin@example.org
run_bms export <person-id> > person.json
run_bms delete <person-id>
run_bms files verify
```

`bms export` and `bms delete` run on the writing copy. `delete` refuses to delete the last access administrator or a superuser's person, and it is recorded in the audit log as the operator. After a delete, the person's uploaded files leave the store within the hour (the `files.reap` job); `bms files verify` shows no outstanding tombstone when that has happened. Backups still hold what was deleted (see [Backups and restore](#backups-and-restore)).

## Upgrading

1. Read the release notes. Migrations only go forward and cannot be undone, so take a backup first (see below).
2. `sudo systemctl stop bms`.
3. Replace `/usr/local/bin/bms` with `sudo install -m 755 bms /usr/local/bin/bms` (or run `sudo restorecon /usr/local/bin/bms` after copying), so a SELinux host gives the file the right label; a binary moved from a home directory keeps its old label and cannot be executed. On SELinux, nginx proxying to a local port may also need `sudo setsebool -P httpd_can_network_connect 1`.
4. Run `bms migrate` (as in the install, as the `bms` user). It applies new migrations in order. Exit status 3 means something is off (see Troubleshooting).
5. `bms check`, then `sudo systemctl start bms`.

A binary older than the database refuses to start (exit 3). To go back to an older version you restore the backup taken before the upgrade; there is no downgrade. With a read-only or on-site copy, upgrade the writer first, then migrate the copies' binaries to match.

## The superuser and break-glass

The superuser is an emergency account, for when nobody can sign in or the last administrator is locked out. It is not for daily work.

```sh
bms create-superuser admin@example.org
```

- The password is generated and **shown once**. Store it somewhere safe.
- Running the command again for the same address resets the password and ends that account's sessions.
- An address that already belongs to a participant account is refused: a superuser is never made by promoting someone.
- Several superuser accounts are allowed, each made by its own command and listed on the accounts screen. A superuser account cannot be changed through the web interface.
- Superuser sessions are short (1 hour by default; the `accounts.superuser_session_lifetime` setting). Every superuser sign-in is logged and audited, and an alert is sent to the addresses in the `notify.alert_addresses` setting (up to ten, comma-separated) when mail is configured.
- In demo mode the demo wipe removes the superuser; run `create-superuser` again afterwards if you need one.

Sign-in and password-reset limits are the `accounts.*` settings (failures per address and per client address, the window, reset lifetime and cooldown). They apply to the next request without a restart. The limiter counts per process: with several `bms serve` processes each keeps its own count.

### The secrets file

`secrets.json` (in the data directory by default) holds the key that signs sign-in cookies and the key that seals the SMTP password. It is created by `bms migrate` only when absent, with mode 0600 (`chmod 0600` is required; bms refuses a file open to group or others).

- Back it up **separately from the database** and keep it as private as the database.
- Copy the same file to every copy of the installation (a read-only or on-site copy), so a sign-in works on any of them.
- If you lose it, bms makes a new one on the next `migrate`: every person is signed out (passwords are kept), and the saved SMTP password can no longer be read, so enter it again on the settings screen.

## Backups and restore

Everything bms holds is in three places. **Back up all three.**

1. **The database** (people, shifts, incidents, audit log, settings, the mail outbox). `pg_dump -Fc bms > bms.dump` is enough for a small installation. How often and where to is yours to decide; no schedule or tool is prescribed.
2. **The data directory**, which includes the secrets file and the audit spool. When `BMS_FILES_STORE=disk`, the uploaded files are inside it (`<files dir>/store`).
3. **The S3 bucket**, if you use `s3`: the bucket's own versioning or replication.

**Order matters when there is no shared snapshot:**

- Dump the database **first**, then copy the files. A file uploaded between the two is then an object with no row, a harmless leftover the daily sweep removes. The other order could leave a row with no file.
- A file deleted between the two leaves a row in the dump whose object is gone. After a restore, `bms files verify` reports it.

**Backups contain everything.** Whoever can read a backup can read every person's data at every sensitivity level, including incident and medical notes. Encrypt backups, restrict who can open them, delete old ones, and remember that a person deleted with `bms delete` is still in older backups. The data directory needs the same protection as the database.

**Restore:**

1. Stop bms. Restore the database into an empty database owned by `bms` (`pg_restore`).
2. Restore the data directory (including `secrets.json`, mode 0600, owner `bms`) and the file store.
3. Run `bms migrate` (it applies any migrations newer than the backup if the binary is newer; with a matching binary it does nothing), then `bms check`.
4. Run `bms files verify`. Fix anything it lists as missing or mismatched (re-upload or restore the file).
5. Start bms.

## Read-only and on-site copies

A copy of the installation can run on a second machine, for example an on-site server at the event that must keep working when the internet is down. A copy reads from a replica of the database.

- bms treats a database as read-only when it is a PostgreSQL standby (in recovery) or when the setting `default_transaction_read_only` is on. It re-checks every 15 seconds. The interface shows a read-only banner, writes answer `503` (`read_only`), and background jobs and the mail sender do nothing.
- To freeze an installation by hand (for example for a migration or before a restore), run `ALTER DATABASE bms SET default_transaction_read_only = on;`, and set it to `off` to thaw it.
- `bms migrate`, `bms create-superuser`, `bms delete` and `bms demo` refuse to run on a read-only copy. Run them on the writer.
- Reads on a read-only copy still work, and reads of sensitive data are still audited. The audit entries cannot go into the database, so they are held in a **spool** on that copy's disk: JSON lines under `<BMS_DATA_DIR>/audit/spool/`. When the copy can write again (it was promoted or the freeze lifted), bms imports them exactly once.
  - The data directory must be writable, backed up and protected like the database.
  - Run **one `bms serve` per data directory**.
  - If the disk is full on a read-only copy, audited reads fail, because an unrecorded read of sensitive data is not allowed.
  - A spool file renamed `.failed` was set aside because it could not be imported (it was unparseable, or the database rejected an entry as invalid). The other files carry on. Look at the warning in the log for the reason. Do not delete the file until you have worked out what it holds: it is audit history. How to repair a `.failed` file is not yet decided; keep it, and ask the maintainers.
  - `bms check` prints how many spool files are waiting and how many are `.failed`.
- Give each copy its own `BMS_INSTANCE_NAME` and the **same** `secrets.json` as the writer.
- **Files on a copy.** For the `disk` store the objects are not in the database, so the copy needs its own copy of `<files dir>/store`. Keeping it in step with the writer is the operator's job: synchronise that directory alongside the database replication. Which tool and schedule to use is not yet decided. For `s3`, point both at a shared bucket or give the site a local replica of it. A file the copy does not have yet shows as "not available on this copy yet" (`503`); `bms files verify` lists them.
- Taking over from a failed writer (a managed switchover) is a later feature (`switchover`); until then it is manual: promote the replica, make sure the secrets file and files are current, run `bms check`.

## Files store

Uploaded files (images and PDFs) are kept outside the database in either:

- **`disk`** (the default): `<BMS_FILES_DIR>/store`, inside the data directory, so the one data volume covers both. Created with mode 0700 by `bms migrate`.
- **`s3`**: any S3-compatible store (self-hosted ones work: SeaweedFS, MinIO, Garage, Ceph). Set the endpoint, bucket, access key and secret key.

Rules for `s3`: the bucket must be **private**. At start-up bms probes it, and **refuses to start** if an anonymous read succeeds. Make it reachable only from the bms hosts. Keep the path-style default `true` unless your store needs otherwise.

What happens on its own: two hourly and daily jobs run on the writing copy. `files.reap` (hourly) removes the bytes of deleted files; `files.sweep-orphans` (daily) removes uploads that never got attached to a record. A row whose file is missing is **never** deleted automatically.

`bms files verify` compares the store with the records and prints four counts (ok, missing, mismatched, unchecked). It exits 0 when nothing is missing or mismatched, 1 otherwise. Run it after a restore or after moving the store. `bms check` prints `files: disk ok` (or the S3 error).

Limits are settings (on the settings screen, for settings administrators): `files.max_bytes` (25 MiB; range 1 MiB to 1 GiB), `files.quota_bytes` (0, unlimited; counted across the whole installation) and `files.max_per_record` (20; 1 to 200).

## Mail

E-mail (password resets, notifications) needs an SMTP server. Without one bms still works; features that need mail say so, and a request to reset a password cannot be answered by e-mail. Everything is set on the settings screen, not in the environment:

| Setting | Default | Meaning |
|---|---|---|
| `notify.smtp_host` | empty (mail off) | Host name or IP of the relay. |
| `notify.smtp_port` | `587` | 1 to 65535. |
| `notify.smtp_security` | `starttls` | `starttls`, `tls`, or `none`. |
| `notify.smtp_username` | empty | Up to 255 characters. |
| `notify.smtp_password` | unset | Sealed at rest (with the key in the secrets file) and never shown back. |
| `notify.sender_address` | empty | The From address, for example `no-reply@example.org`. Mail needs both host and sender. |
| `notify.sender_name` | empty (the regional's name) | The From name. |
| `notify.reply_to` | empty | Optional reply address. |
| `notify.alert_addresses` | empty | Up to ten comma-separated addresses told about superuser sign-ins. |
| `notify.retention` | `0` (forever) | How long to keep sent-mail records, in hours: `0`, or from `168h` (7 days). |

Notes:

- `none` (no encryption) is only for a relay on the same machine. `bms check` warns when it is set.
- Mailpit (in the development compose file) catches mail for developers and is **development only**; never run it for real mail.
- The mail queue lives in the database. A message that cannot be delivered is retried (up to 8 attempts) and expires after 7 days. **Queued messages can hold one-time links** (password resets) until they are sent or expire: protect the database and backups accordingly.
- After a password reset, the person is not signed in automatically; a reset link lasts 1 hour by default (`accounts.password_reset_lifetime`).
- Only the writing copy sends mail.
- Administrators can send a test message, and an outbox screen shows each message's status and any error.

## Jobs

bms runs a few scheduled jobs inside `bms serve`; there is nothing extra to install. They live in the database, so several processes can share the work, and only the writing copy runs them.

| Job | When | What |
|---|---|---|
| `audit.purge` | hourly | Removes audit entries older than `audit.retention` (setting; `0` keeps everything; otherwise at least `8760h`, one year). |
| `jobs.purge` | hourly | Removes finished job records older than 7 days. |
| `files.reap` | hourly | Removes the bytes of deleted files. |
| `files.sweep-orphans` | daily | Removes uploads never attached to a record. |

A job that fails for a temporary reason (database or store unavailable, timeout) is retried with growing delays for about four hours. Any other failure ends the job; `bms check` lists the registered jobs. If the process dies mid-job the job is picked up again within two minutes.

## Demo mode

Demo mode makes an instance safe to open to the public as a try-it-out copy. The demo is a fictional regional with ten shared accounts. **Never turn it on for an instance that holds real people's data.** The commands refuse a database that already has real (non-superuser) accounts, but do not rely on that.

### What it changes

- A banner on every page and a table of demo accounts and one shared password on the sign-in page.
- Mail is captured in the database instead of being sent; each account sees only mail addressed to itself at `/demo/mail`.
- Uploads are capped at 2 MiB whatever `files.max_bytes` says.
- The ten demo accounts are protected: nobody can change their password or address, disable or delete them, or link them to a person. Anything else a tester breaks the next reset repairs.
- Pages are served with `X-Robots-Tag: noindex`.
- It does not change permissions, audit, the privacy filter, validation, cookies or the superuser rules.

### Turn it on

In `/etc/bms/bms.env`:

```
BMS_DEMO_ENABLED=true
BMS_DEMO_PUBLIC_URL=https://demo.example.org
```

On a new database: `bms migrate`, then `bms demo seed` (as the `bms` user, as for `migrate`), then start. `bms check` prints `demo: on (seeded <time>)`. `seed` refuses a database that already holds a non-superuser account (exit 2).

### The reset

`bms demo reset` wipes everything strangers entered (database schemas, uploaded files, the audit spool) and seeds again. The shared accounts' sessions all end, and the superuser is removed (`bms create-superuser` makes a new one). It refuses unless demo mode is on and the database carries the marker the seed wrote (exit 2, nothing touched). It stops being safe to run while the server is running, so the reset **stops bms first**.

`deploy/systemd/bms-demo-reset.service` does that: it skips the run, without failing, when `bms` is not running (an operator stopped it on purpose); otherwise it stops `bms`, runs `bms demo reset`, then starts `bms` again only because it stopped it, also when the reset fails. The run is capped at 30 minutes (`TimeoutStartSec=30min`). `deploy/systemd/bms-demo-reset.timer` runs it at 04:00 every night (with up to ten minutes of random delay). It **ships disabled**. To enable it on a demo instance:

```sh
sudo install -m 0644 deploy/systemd/bms-demo-reset.service deploy/systemd/bms-demo-reset.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now bms-demo-reset.timer
systemctl list-timers bms-demo-reset.timer
```

Run it by hand any time with `sudo systemctl start bms-demo-reset`. The timer never decides on its own: bms runs a reset only when asked.

**Compose or cron equivalent.** A production compose file is not yet written (the one in `deploy/compose` is for development), so this is the pattern, not a tested file: stop the bms service, run the reset in a one-off container with the same environment and volumes, start it again, from the host's cron or a systemd timer, for example `docker compose stop bms && docker compose run --rm bms demo reset; docker compose start bms`. The same rule holds: the server must be stopped while the reset runs.

### If a reset fails

Use the exit status and message:

- **Exit 2:** demo mode is off, or the database was never seeded. Nothing was touched. Set `BMS_DEMO_ENABLED=true`, or run `bms demo seed` first.
- **Exit 1 while clearing files or the spool:** the database is intact. Fix the cause (permissions, a full disk) and run the reset again.
- **Exit 1 at the drop step** (the database refused): the marker stays; fix the role's privileges and run the reset again.
- **The migration step failed** (exit 3 if the schema is behind, ahead or busy, otherwise 1): the demo schemas were dropped and not rebuilt. Run `bms migrate`, then `bms demo seed`.
- **A seed step failed:** the marker stays in "seeding" so the reset is the recovery: run `bms demo reset` again.
- `bms check` shows `demo: on (seeded <time>|not seeded|seeding)`.
- After a failed migrate step, bms will not start until `bms migrate` and `bms demo seed` have been run. The reset unit restarts bms only when it stopped it (also after a failure), so a failed reset can leave the site down until you repair it. bms does not restart-loop on a configuration error (`RestartPreventExitStatus=2`). Until repaired, the demo may be empty or half-seeded.

### Two warnings

- **The banner is not a substitute for keeping real data out.** It tells visitors to use made-up people, but strangers can type anything, and everything entered is visible to every other visitor until the next reset. Look after the demo like a public noticeboard: do not put a real regional's data in the same database, and do not run it on a host that has real backups.
- **Backups of a demo hold whatever strangers typed.** Either do not back a demo up, or treat the backup as holding material you do not control (possibly real personal data a visitor entered against the rules).

## Troubleshooting

First, `bms check`, then `journalctl -u bms`.

- **`bms migrate` stops at people `0003`.** Two people share a playa name, and from this version playa names must be unique. The message lists their IDs. Resolve it **on the previous version** (still running the old binary): have one of them rename, or a profile manager rename them on their behalf. Then run `bms migrate` again. The same migration refuses a database that is not UTF-8; create a UTF-8 database and restore into it.
- **Exit 3 from `serve`, `check` or `migrate`.** The schema is behind the binary (run `bms migrate` on the writer), ahead of it (you have started an older binary: install the newer one or restore a backup), a migration file changed since it was applied (do not edit applied migrations; report it), or another migration holds the lock (wait and retry).
- **Exit 2 and `BMS_X: required`/`invalid`.** `bms check` names the variable. "unknown_variable" means a `BMS_*` name no key reads: a typo. "file_permissions" means the secrets file is open to group or others: `chmod 0600`.
- **`bms migrate` says it refuses a read-only copy.** Run it on the writer.
- **All pages say read-only / `503 read_only`.** The database is a standby, or `default_transaction_read_only` is on. See [Read-only copies](#read-only-and-on-site-copies).
- **Everyone is signed out after a restart.** The secrets file changed or was lost; see [The secrets file](#the-secrets-file).
- **Log line `403 misdirected`.** A request reached bms without passing the front end: its peer is not a loopback address and is not listed in `BMS_HTTP_TRUSTED_PROXIES`. It never fires for a loopback peer. Route the traffic through the front end, firewall the port, or list the front end's address.
- **Warning "a loopback peer that is not in http.trusted_proxies sent X-Forwarded-For; its headers are ignored. If a front end runs on this host, list 127.0.0.1/32".** The front end is on this host but is not listed. Set `BMS_HTTP_TRUSTED_PROXIES=127.0.0.1/32`.
- **Everyone shares one client address / HTTPS headers missing.** Same cause as the line above.
- **bms refuses to start: the files store is public.** Make the S3 bucket private (anonymous reads must fail). An unreachable store does not stop the start: file operations answer `503` until it is back.
- **A file "is not available on this copy yet" / `bms files verify` lists missing.** The file's bytes are not in this copy's store. Sync the store (see Read-only copies) or restore it from backup.
- **Mail is not arriving.** Check the `notify.*` settings and the outbox screen; `bms check` prints the e-mail lines. Messages are retried and then expire after 7 days.
- **`bms demo seed` exits 2 with `not_empty`.** This is the emptiness guard: the database already holds a non-superuser account, and seeding fictional data on top of real data is refused. If it is truly a throw-away database, drop and recreate it, then `migrate` and `seed`. If it holds real data, **do not turn demo mode on there**.
- **`bms demo reset` exits 2 with `not_demo`.** The database was never seeded as a demo (no marker). Nothing was touched.
- **A `.failed` file in the audit spool.** See [Read-only copies](#read-only-and-on-site-copies).

See also [telemetry.md](telemetry.md) for logs, traces and metrics.
