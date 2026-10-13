import createClient from "openapi-fetch";

import { createAuthFetch } from "./auth-fetch";
import type { paths } from "./schema.gen";

// Requests go to /api/v1 on the same origin; next.config.ts forwards them, so the auth
// cookies are first-party (docs/decisions.md).
const baseUrl = "/api/v1";

// Read fetch at call time, so importing this module needs no fetch (jsdom tests).
const browserFetch = (input: Request, init?: RequestInit) => globalThis.fetch(input, init);

// plain sends POST /auth/refresh without the retry logic below.
const plain = createClient<paths>({ baseUrl, fetch: browserFetch });

const sessionLostListeners = new Set<() => void>();

// onSessionLost registers a listener for when the session can no longer be refreshed.
// It returns a function that removes the listener.
export function onSessionLost(listener: () => void): () => void {
  sessionLostListeners.add(listener);
  return () => sessionLostListeners.delete(listener);
}

// The one client for the ResearchSphere API. Paths, parameters and response
// bodies are typed from docs/openapi.yaml (run `make generate` after changing it).
// A 401 renews the session once and retries once (lib/api/auth-fetch.ts).
export const api = createClient<paths>({
  baseUrl,
  fetch: createAuthFetch({
    fetch: browserFetch,
    refresh: async () => (await plain.POST("/auth/refresh")).response.ok,
    onSessionLost: () => sessionLostListeners.forEach((listener) => listener()),
  }),
});

export type { components, paths } from "./schema.gen";
