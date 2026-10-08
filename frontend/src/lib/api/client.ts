import createClient from "openapi-fetch";

import type { paths } from "./schema.gen";

// The one client for the ResearchSphere API. Paths, parameters and response
// bodies are typed from docs/openapi.yaml (run `make generate` after changing it).
// Requests go to /api/v1 on the same origin; next.config.ts forwards them.
export const api = createClient<paths>({ baseUrl: "/api/v1" });

export type { components, paths } from "./schema.gen";
