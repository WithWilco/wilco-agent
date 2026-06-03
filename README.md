# Wilco build agent

`wilco` is a small command-line agent that runs iOS builds for your
[Wilco](https://withwilco.com) account on your own Mac. You install it once with
Homebrew, sign in through your browser, and it connects out to the Wilco server
over a secure WebSocket to receive build jobs and stream logs back.

Nothing listens on your machine — the agent only dials *out*, so there is no
inbound surface to attack. The only credential it holds is a per-machine
enrollment token, and the server stores only a SHA-256 hash of that token, never
the token itself.

## Install

Once the tap is published (see [Publishing the tap](#publishing-the-tap)):

```sh
brew install withwilco/tap/wilco
wilco
```

`wilco` with no arguments runs the guided setup: it checks your build toolchain,
signs you in through the browser, and offers to keep the agent running in the
background.

## What setup does

1. **Checks your toolchain** (`wilco doctor`):
   - Xcode and the Command Line Tools must be present. If they are missing the
     agent tells you to install Xcode from the App Store — it will not try to
     install Xcode for you.
   - Fastlane is required. If it is missing, the agent offers to install it for
     you with `brew install fastlane`.
2. **Signs you in.** If this Mac is not yet connected, the agent opens your
   browser to Wilco and asks you to authorize. Authorization uses a native-app
   loopback OAuth flow (RFC 8252): the browser hands a single-use code to a
   temporary `127.0.0.1` listener, and the agent exchanges that code for its
   enrollment token over HTTPS. The token itself never passes through the
   browser.
3. **Offers to run always.** You can keep the agent running in the background as
   a launchd service (starts at login, restarts on crash), or just run it in the
   current terminal.

## Commands

```
wilco                 Guided setup: check tools, sign in, run the agent
wilco login           Connect this Mac to your Wilco account (opens browser)
wilco logout          Disconnect and stop the background service
wilco doctor          Check Xcode / Command Line Tools / Fastlane
wilco start           Run the agent in the foreground
wilco status          Show login and service status
wilco service ...     install | uninstall | status (run-always background service)
wilco version         Print the version
```

### Background service

`wilco service install` registers a launchd LaunchAgent
(`com.wilco.agent`) that starts at login and restarts on crash. Logs are written
to `~/Library/Logs/wilco/`. Remove it with `wilco service uninstall` (or
`wilco logout`, which also disconnects).

## Configuration

Config is stored in your user config directory
(`~/Library/Application Support/wilco/config.json` on macOS) and is written
during login. You normally never edit it by hand.

Advanced environment overrides:

| Variable        | Default                     | Purpose                       |
| --------------- | --------------------------- | ----------------------------- |
| `WILCO_API_URL` | `https://api.withwilco.com` | API base used for token exchange |
| `WILCO_APP_URL` | `https://withwilco.com`     | Web app base opened in the browser |

## Running against dev vs live

The agent talks to two services: the **API** (where it exchanges the login code
for a token and opens the build WebSocket) and the **web app** (the page your
browser is sent to during `wilco login`).

### Live (production)

You change **nothing**. The shipped binary already points at production
(`https://api.withwilco.com` and `https://withwilco.com`), so end users just run:

```sh
brew install withwilco/tap/wilco
wilco
```

### Dev (local backend + frontend)

Point both bases at your local servers with env vars before logging in — no
rebuild needed:

```sh
export WILCO_API_URL=http://localhost:8000   # backend (FastAPI)
export WILCO_APP_URL=http://localhost:5173   # frontend (Vite dev server)
wilco login
wilco start
```

Against `localhost` / `127.0.0.1` / `::1` the agent is allowed to use a plain
`ws://` build socket; every other host is forced to `wss://`. The backend hands
the agent its socket URL, derived from the backend's own `WILCO_PUBLIC_URL`
(see the backend README), so make sure that env var matches the API host you set
above.

### Changing the compiled-in defaults

If your production domains are different and you want them baked into the binary
(so users don't need env vars), edit the two constants in
[`internal/config/config.go`](internal/config/config.go) (lines 19–20):

```go
const (
	defaultAPIBase = "https://api.withwilco.com"
	defaultAppBase = "https://withwilco.com"
)
```

Then rebuild (`go build .`) and cut a new release.

## Building from source

The project is a single Go binary with vendored dependencies, so it builds
offline:

```sh
go build .
./wilco version
```

## Testing the formula locally

You can install from the formula in this repo without publishing anything:

```sh
brew install --build-from-source ./Formula/wilco.rb
```

To uninstall the local build:

```sh
brew uninstall wilco
```

## Publishing the tap

1. Tag a release in this repository, e.g. `v0.1.0`, and create a source tarball
   (GitHub does this automatically at
   `https://github.com/withwilco/wilco-agent/archive/refs/tags/v0.1.0.tar.gz`).
2. Compute its checksum:

   ```sh
   curl -L https://github.com/withwilco/wilco-agent/archive/refs/tags/v0.1.0.tar.gz | shasum -a 256
   ```

3. Update `url`, `version`, and `sha256` in [`Formula/wilco.rb`](Formula/wilco.rb).
4. Create a tap repository named `withwilco/homebrew-tap` and copy
   `Formula/wilco.rb` into it. Users can then run:

   ```sh
   brew install withwilco/tap/wilco
   ```

## Security notes

- The agent connects only over `wss://` (TLS). Plain `ws://` is refused for any
  non-local server.
- Optional certificate pinning can be configured via the `tls_pin_sha256` config
  field for environments that require it.
- The enrollment token is distinct from the Wilco web session (JWT) and is
  stored server-side as a SHA-256 hash only.
- Builds run as argv-based subprocesses (no shell), and repository URLs,
  branches, and Fastlane lane names are validated before use.
