# AGENTS.md

Technical specification for AI coding assistants working on this project.

## Project Overview

A Go + React single-page application template that compiles into a single binary. The backend embeds frontend build output via `//go:embed`, so production deployment requires only one executable plus a database.

## Tech Stack

- Backend: Go 1.26 + `fox-gonic/fox` + GORM + PostgreSQL (default) / MySQL
- Frontend: React 19 + TypeScript 6 + Vite 8 + Tailwind CSS 4 + Radix Themes
  - React Router v7 (routing)
  - React Query v5 (server state)
  - Axios (HTTP client)
  - Lucide React (icons)

## Development Commands

```bash
task install        # Install backend and frontend dependencies
task dev            # Start development environment (hot reload)
task build          # Build production binary (with embedded frontend)
task build-all      # Cross-compile for multiple platforms
task run            # Run in production mode
task lint           # Auto-fix code style and run checks
task check          # Full checks (backend + frontend types + mod tidy)
task test           # Run tests (race detection + coverage)
task clean          # Remove build artifacts
task update-tools   # Install/update dev tools
```

## Directory Overview

```text
cmd/playground/                      # Application entry point and local config
internal/config/              # YAML config loading (PostgreSQL / MySQL)
internal/database/            # GORM database connection and schema migration
internal/entity/              # Data models and domain types
internal/handler/             # HTTP handlers, route registration, middleware
internal/service/             # Business logic, database operations
internal/errors/              # Centralized error types
pkg/httperr/                  # Generic HTTP-status-aware errors
pkg/id/                       # Prefixed ULID helpers
pkg/secret/                   # Random secret and digest helpers
pkg/strutil/                  # Pure string helpers
pkg/gormlog/                  # GORM logger adapter
website/                      # Embedded SPA (frontend + go:embed glue)
  ├── assets_development.go   #   Dev mode: reverse-proxy to Vite dev server
  ├── assets_production.go    #   Prod mode: go:embed static assets
  ├── package.json
  ├── vite.config.ts
  ├── tsconfig*.json
  ├── eslint.config.js
  ├── vitest.config.ts
  ├── index.html
  ├── public/
  ├── build/                  #   Vite build output (embedded)
  └── src/
      ├── main.tsx
      ├── App.tsx
      ├── router.tsx
      ├── globals.css
      ├── api/
      ├── types/
      ├── views/
      ├── components/
      ├── layouts/
      ├── hooks/
      ├── context/
      └── lib/
scripts/                      # Shell helpers invoked by Taskfile (build, check, tooling)
```

## Core Architecture Constraints

### Backend

- Follow the `Handler -> Service -> Entity` layering
- Register all routes in `internal/handler/handler.go`
- Keep database connection and migration setup in `internal/database/`; services receive a ready `*gorm.DB`
- PostgreSQL (default) and MySQL are supported; switch via `driver` in YAML config
- YAML config contains only bootstrap settings (address, database driver, connection string)
- Configuration files may reference environment variables with `${NAME}` or `${NAME:-fallback}`

### Frontend

- Routing: React Router v7
- Server state management: React Query (`@tanstack/react-query`)
- API calls go in `website/src/api/`
- Type definitions go in `website/src/types/`
- Pages go in `website/src/views/`
- Use Radix Themes directly from `@radix-ui/themes` for shared interactive primitives such as buttons, dialogs, inputs, selects, popovers, menus, tabs, switches, labels, and tables
- Do not hand-roll dropdowns, modal dialogs, or form controls in page files when a Radix Themes primitive exists; compose domain-specific UI around Radix components instead
- Native `<button>`, `<input>`, and `<select>` elements are disallowed by ESLint in page and layout files; use Radix Themes `Button`, `TextField`, `TextArea`, and `Select` primitives instead
- For links that visually behave like buttons, use Radix Themes `Button asChild`
- Default form controls and text buttons should use Radix Themes sizing; do not override control heights in page files unless the interaction explicitly requires it
- Tailwind classes should customize layout and product-specific composition around Radix components, not replace shared component behavior

### Single Binary Embedding

- `website/assets_development.go` (`//go:build development`) reverse-proxies to Vite dev server
- `website/assets_production.go` (`//go:build !development`) serves assets via `//go:embed build/*`
- NotFound handler: `/api` prefix returns JSON 404; all other routes fall back to SPA index
- `task dev` uses fixed local ports by default: `19090` for the Go server and `19173` for Vite; override them explicitly when needed

## Mandatory Rules

- Respect the existing layering and directory structure; do not reshape architecture for local changes
- Run `task check` before committing

## Pre-commit Checklist

- Run `task check`; do not commit if it fails
- Verify whether frontend API calls or types need to be updated accordingly
