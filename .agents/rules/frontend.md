# Frontend Rules

These rules apply to files under `website/src/`.

## Structure

- API calls go in `website/src/api/` and use the shared Axios client.
- Shared HTTP contract types go in `website/src/types/`.
- Page components go in `website/src/views/`.
- Reusable UI belongs in `website/src/components/` or a focused local component folder.
- Route changes should update `website/src/router.tsx`, navigation, and NotFound behavior as needed.

## Server State

- Use React Query for server state.
- Keep query keys stable and scoped to the resource being fetched.
- Invalidate or update relevant queries after mutations.
- Do not hard-code backend origins in components; use the shared API client and Vite proxy.

## UI

- Reuse existing shadcn/ui, Tailwind tokens, Lucide icons, and local layout patterns.
- Cover loading, empty, pending, success, and error states for user-facing data flows.
- Use semantic controls: buttons for actions, labels for form fields, and accessible names for icon-only controls.
- Do not display secrets or tokens in lists or logs; only show them once at creation if the feature requires it.

## Tests

- Prefer Vitest for API clients, hooks, route helpers, and non-trivial UI state derivation.
- Pure display components can skip tests when the behavior is low risk, but they must pass lint and type checks.
