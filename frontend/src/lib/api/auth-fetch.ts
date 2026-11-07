// The fetch the API client uses. When a request gets 401 because the 15-minute access
// token has expired, it renews the session with POST /auth/refresh once and sends the
// request again once (FR-2.1). It never loops: the retry's answer is returned as it is.

type Fetch = (input: Request, init?: RequestInit) => Promise<Response>;

export type AuthFetchOptions = {
  // fetch sends a request; the browser's fetch in the app, a fake in tests.
  fetch: Fetch;
  // refresh calls POST /auth/refresh and reports whether it worked.
  refresh: () => Promise<boolean>;
  // onSessionLost runs when refreshing fails: the session is over.
  onSessionLost: () => void;
};

// Public auth operations: their 401 does not mean an expired access token (a wrong
// password at login, for example), and refresh must not refresh itself.
const noRefresh = new Set([
  "/auth/login",
  "/auth/refresh",
  "/auth/logout",
  "/auth/register",
  "/auth/verify-email",
  "/auth/resend-verification",
  "/auth/password-reset/request",
  "/auth/password-reset/confirm",
]);

function mayRefresh(request: Request): boolean {
  const path = new URL(request.url).pathname.replace(/^\/api\/v1/, "");
  return !noRefresh.has(path);
}

// RefreshOutcome says what POST /auth/refresh answered: ok, refused (401: the session is over)
// or unavailable (no answer, or a server error: nothing is known about the session).
export type RefreshOutcome = "ok" | "refused" | "unavailable";

const refreshLock = "researchsphere-session-refresh";

// createSessionRefresher returns the one way to refresh. A retired refresh token ends the
// session with no grace period (docs/decisions.md, 2026-10-13), and every tab's socket gets 4401
// at the same moment, so callers in this tab share one request and tabs take turns under a Web
// Lock. The browser reads the cookie when each request is sent, so the tab that waited sends the
// rotated token. The Web Locks API exists only on secure origins, and localhost is one; elsewhere
// tabs are not coordinated.
export function createSessionRefresher(send: () => Promise<number>): () => Promise<RefreshOutcome> {
  async function attempt(): Promise<RefreshOutcome> {
    try {
      const status = await send();
      if (status >= 200 && status < 300) return "ok";
      return status === 401 ? "refused" : "unavailable";
    } catch {
      return "unavailable";
    }
  }

  async function inTurn(): Promise<RefreshOutcome> {
    const locks = globalThis.navigator?.locks;
    return locks ? locks.request(refreshLock, attempt) : attempt();
  }

  let inflight: Promise<RefreshOutcome> | undefined;
  return () => {
    inflight ??= inTurn().finally(() => {
      inflight = undefined;
    });
    return inflight;
  };
}

export function createAuthFetch({ fetch, refresh, onSessionLost }: AuthFetchOptions): Fetch {
  // The refresh in progress. Requests that get 401 at the same time share it: the server
  // ends the session when a refresh token is used twice (docs/decisions.md, 2026-10-13).
  let inflight: Promise<boolean> | undefined;
  function refreshOnce(): Promise<boolean> {
    inflight ??= refresh()
      .catch(() => false)
      .finally(() => {
        inflight = undefined;
      });
    return inflight;
  }

  return async (request, init) => {
    // Clone before sending: a body can be read only once.
    const retry = mayRefresh(request) ? request.clone() : undefined;
    const response = await fetch(request, init);
    if (response.status !== 401 || !retry) return response;
    if (!(await refreshOnce())) {
      onSessionLost();
      return response;
    }
    return fetch(retry, init);
  };
}
