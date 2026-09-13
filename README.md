# Phoenix

Phoenix is a lightweight, Go-based self-healing tool for infrastructure. It
watches services over HTTP, TCP or ICMP, decides when one is genuinely down
rather than briefly flaky, and then logs into its host over SSH to run a
recovery action — restarting a container, restarting a systemd unit, or
running a command you supply.

It is designed to be boring on purpose: it only acts when a service crosses a
failure threshold you set, it refuses to hammer a flapping service, and it
gives up and tells you when recovery clearly is not working.

## How it works

```
  ┌──────────┐   every check_interval   ┌──────────┐
  │ Checker  │ ───────────────────────► │ Detector │
  │ http/tcp │      healthy? latency?   │  state   │
  │   ping   │                          │ machine  │
  └──────────┘                          └────┬─────┘
                                             │ HEALTHY → DEGRADED → DOWN
                                             │ (DOWN once failures reach
                                             │  failure_threshold in a row)
                                             ▼
                                    ┌──────────────────┐
                                    │  Recovery policy │  cooldown +
                                    │  (rate limiting) │  max attempts
                                    └────────┬─────────┘
                                             │ allowed?
                                             ▼
                                    ┌──────────────────┐
                                    │    SSH healer    │ docker restart …
                                    │  verified host   │ systemctl restart …
                                    └──────────────────┘
```

Each service is monitored by its own goroutine on its own interval. A single
successful check clears the failure counter, so transient blips never trigger
a recovery.

## Requirements

- Go 1.26 or newer (to build)
- SSH access to each monitored host, using a key-based login
- The `ping` binary on the machine running Phoenix, if you use `ping` checks
- On the remote host: `docker` or `systemctl` available to the SSH user,
  depending on the recovery strategy you pick

## Install

```bash
git clone https://github.com/yudemir1/phoenix.git
cd phoenix
make build        # produces bin/phoenix
```

## Quick start

**1. Copy the example config.**

```bash
cp configs/phoenix.example.yml configs/phoenix.yml
```

**2. Describe your services** in `configs/phoenix.yml` (see the reference
below).

**3. Record each host's SSH key.** Phoenix verifies host keys and will not
connect to a host it cannot recognise, so add every monitored host to your
`known_hosts` first:

```bash
ssh-keyscan -p 22 10.0.0.5 >> ~/.ssh/known_hosts
```

**4. Run it.**

```bash
./bin/phoenix -config configs/phoenix.yml
```

**5. Watch what it does.** Routine checks are logged at `debug`, so start
there while you are still tuning:

```bash
./bin/phoenix -config configs/phoenix.yml -log-level debug
```

## Command-line flags

| Flag | Default | Description |
|---|---|---|
| `-config` | `configs/phoenix.example.yml` | Path to the config file |
| `-log-level` | `info` | `debug`, `info`, `warn` or `error` |

## Configuration

Phoenix rejects unknown keys. A misspelled setting is a startup error, not a
silently ignored line — if Phoenix starts, every key in your file was
understood.

Paths starting with `~/` are expanded for `known_hosts` and `ssh.key_path`.

### `global`

Values under `global` apply to every service, and a service can override the
per-service ones individually.

| Key | Default | Description |
|---|---|---|
| `check_interval` | — | How often to check each service. Required unless every service sets its own. |
| `failure_threshold` | — | Consecutive failures before a service is considered DOWN. Required unless every service sets its own. |
| `timeout` | — | Per-check timeout passed to the checker. |
| `ssh_timeout` | `10s` | Bounds the SSH dial and handshake when connecting to run a recovery. |
| `recovery_cooldown` | `5m` | Minimum time between two recovery attempts for the same service. |
| `recovery_max_attempts` | `3` | Recovery attempts allowed per service inside `recovery_attempt_window`. |
| `recovery_attempt_window` | `30m` | How far back attempts are counted; older ones are forgotten. |
| `known_hosts` | `~/.ssh/known_hosts` | File used to verify SSH host keys. |
| `webhook_url` | — | Where to POST notifications. Empty disables notifications entirely. |
| `webhook_timeout` | `5s` | How long to wait for the notification endpoint. |
| `insecure_skip_host_key_verify` | `false` | Disables host key verification. See [Security](#security). |

### `services`

Each entry describes one monitored service.

| Key | Required | Description |
|---|---|---|
| `name` | yes | Unique identifier, used in logs and for rate limiting. |
| `check_type` | yes | `http`, `tcp` or `ping`. |
| `target` | yes | What to check — see the table below. |
| `check_interval` | no | Overrides `global.check_interval`. |
| `failure_threshold` | no | Overrides `global.failure_threshold`. |
| `timeout` | no | Overrides `global.timeout`. |
| `ssh.host` | yes | Host to connect to for recovery. |
| `ssh.port` | yes | SSH port, 1–65535. |
| `ssh.user` | yes | SSH user. |
| `ssh.key_path` | yes | Private key used to authenticate. |
| `recover.strategy` | yes | `docker_restart`, `systemd_restart` or `custom_command`. |
| `recover.target` | yes | Container or unit name to act on. |
| `recover.command` | only for `custom_command` | Command to run verbatim on the host. |

### Check types

| `check_type` | `target` looks like | Healthy when |
|---|---|---|
| `http` | `https://host/health` | Response status is 200–299 |
| `tcp` | `10.0.0.5:5432` | The TCP connection is accepted |
| `ping` | `10.0.0.9` | A single ICMP echo gets a reply |

### Recovery strategies

| `recover.strategy` | Runs on the remote host |
|---|---|
| `docker_restart` | `docker restart <recover.target>` |
| `systemd_restart` | `sudo systemctl restart <recover.target>` |
| `custom_command` | `<recover.command>`, exactly as written |

A recovery counts as successful when the command exits with status 0.

### Example

```yaml
global:
  check_interval: 10s
  failure_threshold: 3
  timeout: 3s
  recovery_cooldown: 5m
  recovery_max_attempts: 3
  recovery_attempt_window: 30m
  known_hosts: ~/.ssh/known_hosts

services:
  - name: web-api
    check_type: http
    target: "http://10.0.0.5:8080/health"

    ssh:
      host: "10.0.0.5"
      port: 22
      user: "deploy"
      key_path: "~/.ssh/phoenix_id_ed25519"

    recover:
      strategy: docker_restart
      target: "web-api-container"

  - name: worker
    check_type: ping
    target: "10.0.0.9"
    check_interval: 5s
    failure_threshold: 2

    ssh:
      host: "10.0.0.9"
      port: 22
      user: "deploy"
      key_path: "~/.ssh/phoenix_id_ed25519"

    recover:
      strategy: systemd_restart
      target: "worker.service"
```

## Recovery policy

Restarting something automatically is easy to get wrong: a service stuck in a
crash loop looks "down" again a minute after every restart, and a naive tool
would restart it forever while hiding the real problem. Two settings prevent
that, and they solve different problems:

- **`recovery_cooldown` limits the rate.** After a recovery runs for a
  service, further attempts for that service are refused until the cooldown
  expires — no matter how many times it goes down in between.
- **`recovery_max_attempts` decides when to give up.** If recovery has
  already run this many times for a service inside
  `recovery_attempt_window`, Phoenix stops trying and says so in the log.
  When the oldest attempts age out of the window, it becomes willing again.

A refused attempt is logged at `warn` as `recovery skipped by policy`, which
is deliberately distinct from `recovery failed` — one means Phoenix chose not
to act, the other means it tried and the command failed.

## Notifications

Set `webhook_url` and Phoenix will POST a JSON payload whenever something
worth a human's attention happens:

| Event | Sent when |
|---|---|
| `state_changed` | A service moves between HEALTHY, DEGRADED and DOWN |
| `recovery_succeeded` | A recovery command completed successfully |
| `recovery_failed` | A recovery was attempted and failed |
| `recovery_skipped` | The recovery policy refused the attempt (cooldown, or attempts exhausted) |

Routine checks never notify — only the transitions above do.

The payload carries a `text` field holding a human-readable one-liner, plus
the structured fields behind it:

```json
{
  "text": "web-api: recovery FAILED (docker_restart): ssh: connection refused",
  "type": "recovery_failed",
  "service": "web-api",
  "state": "",
  "strategy": "docker_restart",
  "error": "ssh: connection refused",
  "time": "2026-09-13T02:30:00Z"
}
```

That shape works with a Slack incoming webhook, which renders `text` and
ignores the rest, and equally with an endpoint of your own that wants the
structured fields.

Notifications are a side channel: if the endpoint is slow, returns an error
or cannot be reached, Phoenix logs a warning at `warn` and carries on
monitoring and recovering. A broken notification channel never stops
Phoenix from doing its job.

> **`webhook_url` is a secret.** Anyone holding a Slack incoming webhook URL
> can post to that channel. Phoenix never writes it to the log, but it does
> live in your config file — permission that file accordingly
> (`chmod 600`), and keep it out of version control.

## Security

Phoenix logs into your hosts and runs commands there, so it verifies the
identity of every host it connects to against `known_hosts`. There is no
interactive "do you want to continue connecting?" prompt: a host Phoenix
cannot verify is a host it refuses to touch.

This means **Phoenix will not start if `known_hosts` is missing or
unreadable**, and a recovery will fail rather than proceed if a host presents
an unexpected key. Populate the file ahead of time with `ssh-keyscan`.

`insecure_skip_host_key_verify: true` turns verification off. It exists for
throwaway lab environments, and it means an attacker who can intercept the
connection receives your recovery commands and can impersonate your host.
Do not use it against anything you care about.

## Logging

Phoenix logs structured lines via `log/slog`:

```
time=2026-09-13T02:36:26Z level=WARN msg="state changed" service=web-api state=DOWN err="connection refused"
time=2026-09-13T02:36:26Z level=INFO msg="recovery completed" service=web-api strategy=docker_restart target=web-api-container
```

Every line carries the `service` attribute, so you can filter by service.
Routine check results are logged at `debug` to keep the default output
quiet — at `info` you only see state changes and recovery activity.

## Development

```bash
make test      # gofmt check + go vet + the full test suite
make fmt       # rewrite files with gofmt
make vet       # go vet only
make build     # build bin/phoenix
make run       # go run ./cmd/phoenix
make clean     # remove bin/
```

`make test` prints one line per test describing what it checks, and fails if
anything is unformatted or `go vet` reports a problem.

## Project layout

```
cmd/phoenix          entry point: flags, wiring, signal handling
internal/config      config loading, defaults and validation
internal/monitor     health checkers (http, tcp, ping) and their factory
internal/detector    per-service state machine (HEALTHY/DEGRADED/DOWN)
internal/healer      recovery: SSH execution and the rate-limiting policy
internal/runner      per-service loop tying checker, detector and healer together
```

## License

See [LICENSE](LICENSE).
