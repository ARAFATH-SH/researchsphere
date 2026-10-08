# Frontend: Next.js web client (TypeScript)

Next.js (App Router), TypeScript in strict mode, Tailwind CSS, ESLint and Jest. Node.js 22
(see `.nvmrc`; with nvm, run `nvm use`).

## Setup

```bash
cd frontend && npm ci
```

## Commands

Run from the repository root (see `make help`), or the npm script from `frontend/`.

| Make | npm | What it does |
| --- | --- | --- |
| `make web` | `npm run dev` | Dev server on http://localhost:3000 |
| `make test-web` | `npm test` | Jest tests |
| `make lint-web` | `npm run lint`, `npm run typecheck` | ESLint and the TypeScript type check |
| `make generate` | `npm run generate` | Regenerate `src/lib/api/schema.gen.ts` from `docs/openapi.yaml` |
| | `npm run build` | Production build |

## Talking to the API

The browser calls `/api/v1/...` on this origin. `next.config.ts` forwards those requests to
`API_URL` (default `http://localhost:8080`), so start the API with `make api`. Call the API
only through `src/lib/api/client.ts`.

## Layout

- `src/app`: routes (placeholder pages for every page in SRS 5.1)
- `src/components`: shared components
- `src/lib/api`: the API client and the generated types
- `src/features/<module>`: code for one feature, named after the backend module

`AGENTS.md` in this folder is written by `next dev` for AI assistants; the project rules are
in the root `AGENTS.md`.
