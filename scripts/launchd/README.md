# launchd scheduling for mail-daily-digest V3

This replaces the old `cron` schedule with `launchd`. The reason is the
sleep gap: macOS `cron` does NOT fire while the Mac is asleep and never
replays a missed job, which is why the 2026-06-15 07h report was lost.
`launchd` with `StartCalendarInterval` runs a missed calendar slot ONCE
after the Mac wakes, closing that gap.

The organizer itself now runs inside Docker (`docker compose run --rm
--no-deps organizer`), driven by the `scripts/run-organizer.sh` wrapper,
which waits for the network to come up before invoking the container.

## Files

| File | Purpose | Schedule |
|------|---------|----------|
| `com.nyc.mail-digest.organize.plist` | organize pass | 07:00 and 19:00 local |
| `com.nyc.mail-digest.report.plist` | digest + counter reset | 07:30 and 19:30 local |

Both point at `<repo>/scripts/run-organizer.sh` and log to
`~/.local/logs/mail-daily-digest.log`. launchd does not expand `~` or `$HOME`,
so the plists ship with a `__HOME__` placeholder that the install step
replaces with your home directory. They assume the repo is cloned at
`~/repos/mail-daily-digest`.

## What the wrapper guards against

`scripts/run-organizer.sh` exists because a launchd job fires the moment the
Mac wakes, often before the network is up: DNS fails, the OAuth refresh 500s
and the run dies. Before invoking the container it:

1. Puts the Docker CLI on `PATH` (launchd's default `PATH` does not include
   `/usr/local/bin`, `/opt/homebrew/bin` or the Docker Desktop bin dir).
2. Starts Colima if the Docker daemon is down. Colima does not reliably
   auto-start on boot (its brew-services agent has been seen in an error
   state), so a stopped runtime would otherwise kill every scheduled run.
3. Waits for `oauth2.googleapis.com` to resolve, up to 10 tries 6 s apart, and
   aborts non-zero if it never does.
4. Runs `docker compose run --rm --no-deps organizer`, passing any extra args
   through, and appends everything to the log.

Trap: bash sets `$UID` itself and marks it readonly, so `UID=$(id -u) docker
compose ...` fails with `UID: readonly variable`. The wrapper exports the
`UID` bash already holds and computes only `GID`.

## Ollama endpoint (important, no config.json edit needed)

`docker-compose.yml` pins `OLLAMA_ENDPOINT=http://ollama:11434` (the compose
`ollama` service). That service has NO models pulled (it would need an ~8 GB
pull). The host already runs Ollama on `localhost:11434` with `gemma3:4b` and
`gemma3:12b` pulled.

So the wrapper exports `OLLAMA_ENDPOINT=http://host.docker.internal:11434`,
which points the container at the HOST's Ollama and reuses the already-pulled
models. The Go code lets the `OLLAMA_ENDPOINT` env var win over `config.json`
(`internal/config/config.go`), so:

- You do NOT need to edit `config.json`.
- You do NOT need to start the compose `ollama` service (the wrapper uses
  `--no-deps` so `docker compose` will not spin it up).

The only requirement is that the host's Ollama is serving on `:11434` (it
already is). If you ever stop the host Ollama, the container runs will fail
the model Ping and exit non-zero (logged), which is the intended fail-loud
behavior.

## Install

```bash
# 1. Make sure the wrapper is executable (it ships +x, this is a safety net).
chmod +x ~/repos/mail-daily-digest/scripts/run-organizer.sh

# 2. Render the plists into your user LaunchAgents dir.
for job in organize report; do
  sed "s#__HOME__#$HOME#g" ~/repos/mail-daily-digest/scripts/launchd/com.nyc.mail-digest.$job.plist \
    > ~/Library/LaunchAgents/com.nyc.mail-digest.$job.plist
done

# 3. Load them (the -w flag marks them enabled persistently).
launchctl load -w ~/Library/LaunchAgents/com.nyc.mail-digest.organize.plist
launchctl load -w ~/Library/LaunchAgents/com.nyc.mail-digest.report.plist

# 4. Confirm they are registered.
launchctl list | grep mail-digest
```

## Test a run on demand

`kickstart -k` force-runs the job right now (the `-k` kills any running copy
first), independent of the calendar slot:

```bash
# Run the organize pass now.
launchctl kickstart -k gui/$(id -u)/com.nyc.mail-digest.organize

# Run the report pass now.
launchctl kickstart -k gui/$(id -u)/com.nyc.mail-digest.report

# Watch the log.
tail -f ~/.local/logs/mail-daily-digest.log
```

The first container run builds the `mail-daily-digest:3.0.0` image, so it
will be slower; later runs reuse the image. Make sure Docker Desktop is
running (the wrapper aborts non-zero with a logged error if it is not).

## Uninstall

```bash
launchctl unload -w ~/Library/LaunchAgents/com.nyc.mail-digest.organize.plist
launchctl unload -w ~/Library/LaunchAgents/com.nyc.mail-digest.report.plist
rm ~/Library/LaunchAgents/com.nyc.mail-digest.organize.plist
rm ~/Library/LaunchAgents/com.nyc.mail-digest.report.plist
```
