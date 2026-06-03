# Wilco build agent

`wilco` lets [Wilco](https://withwilco.com) build your iOS apps on **your own
Mac**. iOS apps can only be built on Apple hardware, so instead of uploading
your code to a cloud machine, Wilco runs the build locally on the Mac you
already use for development — with Xcode, your signing certificates, and your
provisioning profiles right where they belong.

You install the agent once with Homebrew and sign in through your browser. From
then on, when you start an iOS build in the Wilco web app, the job runs on your
Mac and the live build log streams back to your browser.

## Why it's safe to run

The agent is built so that running it adds **no inbound attack surface** to your
machine:

- **It only dials out.** Nothing listens on your Mac for incoming connections.
  The agent opens a single outbound, encrypted (`wss://` / TLS) connection to
  Wilco and waits for build jobs. There is no open port for anyone to reach.
- **It can't be impersonated cheaply.** The only credential it stores is a
  per-machine enrollment token. The Wilco server keeps only a **SHA-256 hash**
  of that token — never the token itself — so the server database can't leak it.
- **The token never touches your browser.** Sign-in uses a native-app loopback
  flow (RFC 8252): your browser hands a single-use code to a temporary
  `127.0.0.1` listener on your Mac, and the agent exchanges that code for its
  token directly over HTTPS.
- **Build inputs are validated, and nothing runs through a shell.** Repository
  URLs, branch names, Fastlane lanes, and Xcode scheme names are checked against
  a strict allowlist, and commands are executed as argument lists (never a shell
  string), so a malformed or spoofed job can't inject commands onto your Mac.
- **You're always in control.** Disconnect any time with `wilco logout`, which
  revokes this Mac's token on the server and stops the background service.

## Install

```sh
brew install withwilco/tap/wilco
wilco
```

Running `wilco` with no arguments starts the guided setup. It:

1. **Checks your build tools** — Xcode and the Command Line Tools must be
   installed (the agent won't install Xcode for you; get it from the App Store).
   Fastlane is also required; if it's missing, the agent offers to install it
   with `brew install fastlane`.
2. **Signs you in** — opens your browser to authorize this Mac. Once you approve,
   the Mac is connected to your Wilco account.
3. **Offers to run in the background** — keep the agent running as a login
   service so builds work whenever you're signed in, or just run it in the
   current terminal.

That's it. You can now trigger iOS builds from the Wilco web app.

## How a build works

When you start an iOS build for a repository in the Wilco web app:

1. **Wilco detects iOS repos automatically.** Repositories whose primary
   language is Swift or Objective-C build here on your Mac; everything else uses
   Wilco's cloud CI. You don't have to choose.
2. **You pick an Xcode scheme.** On the first build for a repo, the agent clones
   it and lists the available Xcode schemes so you can choose which one to build.
   Your choice is **remembered per repository** and reused on later builds — you
   can change it any time from the build screen.
3. **The build runs and streams back.** The agent clones the selected branch,
   installs dependencies (`bundle install` / `pod install` when present), runs
   Fastlane with your chosen scheme, and streams every log line to your browser
   in real time.

## Commands

```
wilco                 Guided setup: check tools, sign in, run the agent
wilco login           Connect this Mac to your Wilco account (opens browser)
wilco logout          Disconnect this Mac and stop the background service
wilco doctor          Check Xcode / Command Line Tools / Fastlane
wilco start           Run the agent in the foreground
wilco status          Show login and service status
wilco service ...     install | uninstall | status (background service)
wilco version         Print the version
```

### Run in the background

`wilco service install` registers a login service (`com.wilco.agent`) that
starts when you log in and restarts automatically if it crashes, so your Mac is
always ready to build. Logs are written to `~/Library/Logs/wilco/`.

Remove it with `wilco service uninstall`, or `wilco logout` (which also
disconnects this Mac from your account).

## Where your settings live

Configuration is written during sign-in to
`~/Library/Application Support/wilco/config.json`. You normally never edit it by
hand — `wilco login` and `wilco logout` manage it for you.

## Troubleshooting

- **"No build agent is online"** in the web app — make sure the agent is running
  (`wilco status`) and signed in (`wilco login`). If you set up the background
  service, check `~/Library/Logs/wilco/`.
- **Build fails immediately** — run `wilco doctor` to confirm Xcode, the Command
  Line Tools, and Fastlane are all installed.
- **No schemes found** — the agent looks for an `.xcworkspace` or `.xcodeproj` on
  the selected branch. Confirm the project builds locally with Xcode first.

## Uninstall

```sh
wilco logout          # disconnect this Mac and stop the service
brew uninstall wilco
```
