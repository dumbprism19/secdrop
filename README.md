# secdrop

[![CI](https://github.com/dumbprism19/secdrop/actions/workflows/ci.yml/badge.svg)](https://github.com/dumbprism19/secdrop/actions/workflows/ci.yml)

A keyboard-first terminal app for [Berglas](https://github.com/GoogleCloudPlatform/berglas) secrets on Google Cloud.

Instead of remembering bucket paths and typing `berglas access my-bucket/payments-api-prod-env`,
open secdrop, type a few letters of the service, and see its keys, masked, per environment.
Reveal, copy, edit, compare environments, look back through history, and check pod health,
without secrets ever landing in your terminal scrollback or logs.

Works for a whole organization or for one person with a single bucket.

## Features

- **Finds your secrets on its own.** On first run secdrop searches every Google Cloud project
  you can see for buckets that hold Berglas secrets, and remembers the one you pick.
- **Services and environments, not object paths.** Names like `payments-api-prod-env`,
  `payments_api_production` or `payments-api/dev.env` are grouped into a service with its
  environments (dev, staging, nonprod, prod and other common names).
- **Masked by default.** Values show as dots. Reveal one or all of them; they hide again after
  20 seconds.
- **Copy a value or a whole env file.** The clipboard is cleared after 30 seconds.
- **Safe edits.** Add, edit or delete a key with a review step before every save. A save only
  succeeds if nobody changed the secret since you opened it, and untouched lines of the file
  are written back exactly as they were.
- **Compare environments** by fingerprint, without showing plaintext: see keys missing from
  prod, values identical across environments, and copy a missing key across in one step.
- **History and undo.** Every stored version, which keys changed in it, who saved it and who
  read it (from Cloud Audit Logs), and a one-step restore.
- **Pod health** for each service from your current `kubectl` context, matched to services
  even behind Helm release prefixes.
- **⌘K / ctrl+K palette** with Services, Pod health and Search, plus your workspaces.
- **Workspaces.** Save the services you use most as a named workspace ("Payments team") and
  open them from ⌘K without searching. Stored locally per bucket; names only, no secrets.
- **Scriptable.** `secdrop run` starts a command with a service's secrets in its environment,
  without writing them to disk. `secdrop export` writes an owner-only `.env` file.

## Requirements

- Secrets stored with Berglas in **Cloud Storage** (the `berglas://bucket/object` form).
- The [gcloud CLI](https://cloud.google.com/sdk/docs/install), signed in. secdrop can run the
  sign-in for you.
- Go 1.26 or later, to build from source. Older Go versions download the right toolchain
  automatically.
- Optional: `kubectl` with a current context, for pod health.

## Install

With Go:

```sh
go install github.com/dumbprism19/secdrop/cmd/secdrop@latest
```

This puts `secdrop` in `~/go/bin`; make sure that directory is on your `PATH`.

Or from source:

```sh
git clone https://github.com/dumbprism19/secdrop.git
cd secdrop
go build -o bin/secdrop ./cmd/secdrop
./bin/secdrop
```

## Getting started

1. Run `secdrop`. After the intro it checks your Google Cloud sign-in. If you're signed out,
   press enter to run `gcloud auth login --update-adc`, which opens your browser.
2. The first time, pick your secrets bucket from the list secdrop finds, or type its name.
   secdrop remembers it in `~/.config/secdrop/config.json`. Nothing secret is stored there.
3. Type to search for a service, then press enter to open it.

```sh
secdrop                   # opens the bucket you used last
secdrop --pick            # choose a bucket again
secdrop --bucket NAME     # open a specific bucket
secdrop --no-motion       # no animation (or SECDROP_NO_MOTION=1)
```

## Keys

| Key | Where | Action |
| --- | --- | --- |
| type | home | Search services |
| `↑` `↓` · `enter` · `esc` | everywhere | Move · open · go back |
| `tab` | everywhere | Switch environment |
| `w` / `ctrl+w` | service / home | Your workspaces, with **+ New workspace** at the top |
| `⌘K` / `ctrl+k` | everywhere | Services, pod health, search and every other action |
| `enter` | service | Reveal the selected value |
| `v` | service | Reveal every value |
| `y` | service | Copy the selected key as `export KEY=value` |
| `c` | service | Copy the whole env file |
| `e` | service | Edit a value |
| `/` | service | Search keys |
| `d` | service | Compare with another environment; `enter` on a missing key copies it across |
| `h` | service | History; `enter` shows what changed, `r` restores |
| `?` | service | Help |
| `ctrl+c` | everywhere | Quit |

Add key, delete key, export, switch bucket, reload and sign in again are in the ⌘K palette.

Press `w` (or `ctrl+w` on the home screen, where letters go to the search box) to see your
workspaces and create new ones. They're also in ⌘K, to the right of the cards: `→` moves into the list, `↑↓` picks one,
and enter opens it. Choose **+ New workspace** to name one and tick its services with space.
Inside a workspace, enter on a service opens its secrets; the list also has rows to edit or
delete the workspace.

When adding a key you can paste instead of typing: a line such as `export KEY=value`,
`export KEY = 'two words'` or `KEY="value"` fills in the key and value, and a pasted block of
several lines adds them all in one save. Keys that already exist are marked and replaced.

macOS terminals use ⌘K themselves, so it never reaches the app by default. Run
`secdrop setup-keys` once: in Cursor and VS Code it forwards ⌘K to secdrop only while a
full-screen app is open, so ⌘K still clears your shell. It also prints the setting for
Ghostty, kitty, WezTerm and iTerm2. `ctrl+k` works everywhere without setup.

## Command line

These use the bucket you picked last, or `--bucket NAME`. Environment names accept common
aliases, so `production` and `prod` are the same.

```sh
# Start a command with a service's secrets as environment variables. Nothing touches disk.
secdrop run payments-api dev -- npm start

# Write an env file readable only by you. Existing files are never overwritten.
secdrop export payments-api dev -o .env.local

# Print it instead, for piping.
secdrop export payments-api dev
```

## Permissions

secdrop always acts as you, with your own Application Default Credentials. It never uses a
service account of its own and stores no tokens.

| To | You need |
| --- | --- |
| List and read secrets | `roles/storage.objectViewer` on the bucket and `roles/cloudkms.cryptoKeyDecrypter` on the Berglas key |
| Edit, add, delete, restore | `roles/storage.objectAdmin` on the bucket and `roles/cloudkms.cryptoKeyEncrypter` on the key |
| Find buckets automatically | Permission to list projects and buckets. Without it, type the bucket name |
| See who read or saved a secret | Cloud Storage Data Access audit logs turned on for the project, and `roles/logging.privateLogViewer` |
| Keep older versions for history | Object Versioning turned on for the bucket |
| Pod health | `kubectl` access to list pods in your current context |

## How secrets are handled

- Lists hold names and fingerprints only. A value is decrypted when you open a service, held
  in memory, and never written to disk unless you export it.
- Plaintext lives in a type whose `String`, `GoString`, `Format` and JSON encoders print
  `[redacted]`, so it can't leak into logs or error messages. The Berglas library's own
  logging is switched off.
- The app runs in the terminal's alternate screen, so nothing stays in scrollback after you quit.
- Fingerprints are HMAC-SHA256 with a key generated fresh each session, so they can't be
  matched across sessions or brute-forced from a screenshot.
- Every write is conditional on the version you opened, so a concurrent change is never
  silently overwritten.
- Opening a secret is a real Cloud KMS decrypt under your account and shows up in your
  project's audit logs like any other access.

## Limitations

- Berglas secrets in Secret Manager (`sm://`) aren't supported yet.
- Pod health shows the current kubectl context, which may not match the environment you
  are looking at. The cluster name is shown next to it.
- `secdrop run` and `secdrop setup-keys` are built for macOS and Linux.

## Development

```sh
go test ./...
go vet ./...
go build -o bin/secdrop ./cmd/secdrop
```

The code lives in `internal/`: `ui` (screens), `store` (Berglas and Cloud Storage), `dotenv`
(line-preserving `.env` parsing), `secret` (the redacting value type), `kube` (pod health),
`gcp` (sign-in) and `keysetup` (⌘K forwarding).

## Security

Please report vulnerabilities privately; see [SECURITY.md](SECURITY.md).

## License

Copyright 2026 dumbprism. Licensed under the Apache License 2.0; see [LICENSE](LICENSE)
and [NOTICE](NOTICE).
