# Goblet Architecture Rules

These rules keep Goblet useful as a small Go + React single-binary template.

## Principles

- Preserve the existing `Handler -> Service -> Entity` layering.
- Keep route registration centralized in `internal/handler/handler.go`.
- Keep database connection and migration setup in `internal/database/`.
- Put application models in `internal/entity/`; do not expose entities directly as HTTP contracts.
- Put reusable, business-agnostic helpers in `pkg/`, with small APIs and tests.
- Avoid adding product-specific modules, integrations, or abstractions until a template use case needs them.

## Backend Boundaries

- Handlers own HTTP binding, status codes, response DTOs, and route grouping.
- Services own business logic and database access.
- Entities own GORM models, table names, persistence constants, and narrow model helpers.
- Configuration should stay bootstrap-focused: listen address, database driver, DSN, and similarly necessary startup settings.
- PostgreSQL and MySQL support must stay explicit; if a feature only works with one driver, reject unsupported drivers early.

## Frontend Boundaries

- API calls live in `website/src/api/`.
- Shared HTTP contract types live in `website/src/types/`.
- Page-level routes live in `website/src/views/`.
- Reuse the existing React Router, React Query, shadcn/ui, Tailwind, and Axios patterns.
- Do not hard-code backend origins in components; use the Vite proxy and shared API client.

## Change Checks

- Start code changes with `git status --short`.
- Keep changes small and template-oriented.
- Run `task check` before committing.
- Run `task test` when behavior, API contracts, database models, or UI interactions change.
