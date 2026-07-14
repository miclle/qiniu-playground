# Qiniu Playground

Qiniu Playground is a sandbox-based web IDE and terminal for GitHub repositories.
It will let users sign in with GitHub, install a GitHub App, configure their
Qiniu Cloud API key, open authorized repositories in a sandbox, use code-server
as an online IDE, and attach a browser terminal through a sandbox PTY.

The project is based on [`miclle/goblet`](https://github.com/miclle/goblet): a
Go + React single-page application that compiles into one deployable binary.
The backend embeds the frontend build output via `//go:embed`, so production
deployment is typically a single executable plus a database.

## Tech Stack

**Backend:**

- Go 1.26, [fox-gonic/fox](https://github.com/fox-gonic/fox) (Gin-based HTTP framework)
- GORM with PostgreSQL (default) or MySQL driver
- [Viper](https://github.com/spf13/viper) for configuration

**Frontend:**

- React 19, TypeScript 6, Vite 8, Tailwind CSS 4
- React Router v7, React Query v5
- [Radix Themes](https://www.radix-ui.com/themes) component library
- Vitest 4 for unit testing

## Requirements

- Go 1.26+
- Node.js 22.14+
- PostgreSQL or MySQL
- [Task](https://taskfile.dev/) (task runner)
- `reflex` — file-watching hot reload for `task dev`
- `staticcheck` — static analysis for `task check`
- `golangci-lint` — comprehensive linting for `task check`

Run `task update-tools` to install `reflex`, `staticcheck`, and `golangci-lint`.

## Roadmap

The implementation is being delivered in verified phases. Each phase includes
tests, documentation, and a focused commit.

1. Initialize from the Goblet template. (Done)
2. Add GitHub OAuth login. (Done)
3. Add GitHub App installation and repository listing. (Done)
4. Add encrypted Qiniu Cloud API key configuration. (Done)
5. Create or connect sandboxes through `qiniu/go-sdk`. (Done)
6. Open repositories in sandboxes and expose code-server. (Done)
7. Attach a web terminal through a sandbox PTY and xterm.js. (Done)
8. Polish the simple workspace UI. (Done)

## Current Capabilities

- GitHub OAuth login with signed application sessions.
- GitHub App installation flow and authorized repository sync.
- Encrypted Qiniu Sandbox API key storage.
- Sandbox create/connect through `github.com/qiniu/go-sdk/v7/sandbox`.
- Repository open flow that clones code into a sandbox and starts code-server.
- Browser terminal powered by sandbox PTY and xterm.js.

## Quick Start

```bash
git clone https://github.com/miclle/qiniu-playground.git
cd qiniu-playground
task install
```

Create local config:

```bash
cp cmd/playground/config.example.yaml cmd/playground/config.local.yaml
# Edit config.local.yaml with your database settings
```

Start development:

```bash
task dev
```

This starts both the Vite dev server and the Go server with hot reload. By
default, `task dev` uses port `19090` for the Go server and `19173` for Vite.
If either port is already in use, startup stops with a clear error.

To require exact ports for a session, set them explicitly:

```bash
QINIU_PLAYGROUND_HTTP_PORT=19190 QINIU_PLAYGROUND_VITE_PORT=19174 task dev
```

`task dev` wires those values through Vite, the Vite `/api/v1` proxy, the
development asset reverse proxy, and the default GitHub OAuth redirect URL.

## Common Commands

```bash
task install        # Install Go and frontend dependencies
task dev            # Start Vite dev server + Go hot reload
task build          # Build production binary with embedded frontend
task build-all      # Cross-compile for linux/darwin/windows × amd64/arm64
task run            # Run the production binary with local config
task lint           # Auto-fix: go mod tidy, gofmt, go vet, staticcheck, ESLint
task check          # CI-aligned checks (read-only, no file modifications)
task test           # Go tests (race + coverage) + frontend Vitest
task clean          # Remove build artifacts
task update-tools   # Install/update reflex, staticcheck, golangci-lint
```

## Architecture

```
.
├── cmd/playground/                        → Application entry point
│   ├── main.go
│   └── config.example.yaml
├── internal/
│   ├── config/                     → YAML config loading (Viper)
│   ├── entity/                     → Data models (GORM)
│   ├── handler/                    → HTTP handlers + routes + middleware
│   ├── service/                    → Business logic + DB operations
│   └── errors/                     → Centralized error types
├── pkg/gormlog/                    → GORM logger adapter
├── website/                        → Embedded SPA (React + Vite)
│   ├── assets_development.go       → Dev: reverse-proxy to Vite dev server
│   ├── assets_production.go        → Prod: //go:embed build/*
│   └── src/
│       ├── api/                    → API client (Axios)
│       ├── components/             → Domain-specific React components
│       ├── context/                → React context providers
│       ├── hooks/                  → Custom React hooks
│       ├── layouts/                → Page layout components
│       ├── lib/                    → Utilities (React Query client, cn helper)
│       ├── types/                  → TypeScript type definitions
│       └── views/                  → Page-level route components
├── scripts/                        → Shell helpers invoked by Taskfile
├── .github/workflows/              → CI workflows
└── Taskfile.yaml                   → Task runner configuration
```

### Single Binary Embedding

The key pattern: two Go files with build tags control how frontend assets are served:

- **Development** (`-tags development`): Reverse-proxies requests to Vite dev server at `localhost:19173`
- **Production** (default): Serves assets from `//go:embed build/*`, with SPA fallback for non-API routes

## Configuration

The YAML config (`config.example.yaml`) keeps bootstrap settings and OAuth
credentials:

```yaml
addr: "0.0.0.0:${QINIU_PLAYGROUND_HTTP_PORT:-19090}"
driver: postgres   # or "mysql"
dsn: "host=localhost port=5432 user=postgres password=postgres dbname=app sslmode=disable"

auth:
  session_secret: "${QINIU_PLAYGROUND_SESSION_SECRET:-change-me-in-local-config}"
  encryption_key: "${QINIU_PLAYGROUND_ENCRYPTION_KEY:-change-me-in-local-config}"

github:
  oauth_client_id: "${GITHUB_OAUTH_CLIENT_ID:-}"
  oauth_client_secret: "${GITHUB_OAUTH_CLIENT_SECRET:-}"
  oauth_redirect_url: "${GITHUB_OAUTH_REDIRECT_URL:-http://localhost:19090/api/v1/auth/github/callback}"
  app_id: "${GITHUB_APP_ID:-0}"
  app_slug: "${GITHUB_APP_SLUG:-}"
  app_private_key_path: "${GITHUB_APP_PRIVATE_KEY_PATH:-}"

sandbox:
  endpoint: "${QINIU_SANDBOX_ENDPOINT:-}"
  default_template_id: "${QINIU_SANDBOX_TEMPLATE_ID:-base}"
  default_timeout_seconds: 86400
```

For local development, create a GitHub OAuth app with this callback URL:

```text
http://localhost:19090/api/v1/auth/github/callback
```

The application stores local user accounts separately from OAuth identities.
GitHub login names and emails are display metadata; authorization is keyed by
the stable GitHub user ID returned by GitHub's `/user` API.

Create a GitHub App for repository access with this setup callback URL:

```text
http://localhost:19090/api/v1/github/app/callback
```

The app needs repository access for Contents so Qiniu Playground can later clone
selected repositories inside sandboxes. The backend signs a short-lived GitHub
App JWT, exchanges it for an installation token, and uses that token to call
GitHub's installation repositories API.

If the GitHub App was already installed for the signed-in account, use the same
app entry point to configure repository access and save the installation
callback locally. The workspace then refreshes repositories from the stored
installation instead of asking the user to reinstall the app.

## Authentication API

```text
GET  /api/v1/auth/github/login     Redirect to GitHub OAuth
GET  /api/v1/auth/github/callback  Exchange code, create account, set session
GET  /api/v1/auth/me               Return the signed-in user
POST /api/v1/auth/logout           Clear the local session
```

Sessions are application-owned signed cookies. GitHub access tokens are used
only to fetch the user profile during callback handling.

## Qiniu Credential API

```text
GET    /api/v1/qiniu/credentials  Return whether credentials are configured
PUT    /api/v1/qiniu/credentials  Encrypt and store Qiniu credential settings
DELETE /api/v1/qiniu/credentials  Delete stored credentials
```

`sandbox_api_key` is required because sandbox create/connect calls depend on it.
`maas_api_key`, `access_key`, and `secret_key` are optional settings for later
Qiniu service integrations. Stored Qiniu Cloud credentials are encrypted with
AES-GCM before they are written to the database. API responses return only
configuration status and short key hints such as `...abcd`.

## Sandbox API

Qiniu Playground uses `github.com/qiniu/go-sdk/v7/sandbox` to create and connect
sandbox instances with the user's stored API key.

```text
GET  /api/v1/templates                     List sandbox templates from Qiniu Sandbox
GET  /api/v1/sandboxes                     List sandbox sessions
POST /api/v1/sandboxes                     Create a sandbox and wait until ready
GET  /api/v1/sandboxes/:sandboxID/pty      WebSocket bridge to a sandbox PTY
POST /api/v1/sandboxes/:sandboxID/connect  Connect to an existing sandbox
```

The default sandbox template is `base` and the default timeout is 86400 seconds.
Both can be changed in `cmd/playground/config.local.yaml`.
The web terminal uses xterm.js and sends raw terminal input over the PTY
WebSocket. Browser WebSocket upgrades are accepted only from the same host as
the application server.

## Workspace API

```text
POST /api/v1/repositories/:repositoryID/open  Create a sandbox, clone the repository, and start code-server
```

Opening a repository uses the GitHub App installation token for a short-lived
clone URL inside the sandbox. The backend starts code-server on port `8080` and
returns the sandbox public host as the IDE URL.

## GitHub App API

```text
GET /api/v1/github/app/install   Return the GitHub App installation URL
GET /api/v1/github/app/callback  Store the installation selected by the user
GET /api/v1/github/installations List connected installations
GET /api/v1/github/repositories  Sync and return authorized repositories
```

Repository sync is scoped to the signed-in account and removes repositories that
are no longer returned by the GitHub installation repositories API.

## Build & Deployment

```bash
task build          # Build production binary (includes frontend)
./bin/qiniu-playground -c config.yaml
```

Cross-compile for all supported platforms:

```bash
task build-all      # Outputs to bin/ for each OS/arch combination
```

## CI

GitHub Actions workflows are included:

- **ci.yml** — runs backend gofmt/vet/staticcheck/tests, frontend lint/type-check/tests/build, and an embedded binary build
- **golangci-lint.yml** — runs golangci-lint on PRs
- **dependency-review.yml** — reviews dependency changes on PRs
- **actionlint.yml** — lints GitHub Actions workflow files

## License

[MIT](LICENSE)
