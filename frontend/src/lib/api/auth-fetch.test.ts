/**
 * @jest-environment node
 */
// Node, not jsdom: the test needs the standard Request and Response classes.
import { createAuthFetch, createSessionRefresher } from "./auth-fetch";

const url = (path: string) => `http://localhost:3000/api/v1${path}`;
const status = (code: number) => new Response(null, { status: code });

// setup returns an auth fetch whose network answers with the given statuses in order.
function setup(answers: number[], refreshWorks = true) {
  const queue = [...answers];
  const sent: { path: string; body: string }[] = [];
  const fetch = jest.fn(async (request: Request) => {
    sent.push({ path: new URL(request.url).pathname, body: await request.text() });
    return status(queue.shift() ?? 500);
  });
  const refresh = jest.fn(async () => refreshWorks);
  const onSessionLost = jest.fn();
  return { authFetch: createAuthFetch({ fetch, refresh, onSessionLost }), fetch, refresh, onSessionLost, sent };
}

describe("createAuthFetch", () => {
  it("passes other answers through without refreshing", async () => {
    const { authFetch, refresh } = setup([200]);
    expect((await authFetch(new Request(url("/auth/me")))).status).toBe(200);
    expect(refresh).not.toHaveBeenCalled();
  });

  it("refreshes once on 401 and retries the request once with the same body", async () => {
    const { authFetch, refresh, onSessionLost, sent } = setup([401, 200]);
    const request = new Request(url("/posts"), { method: "POST", body: '{"text":"hi"}' });
    expect((await authFetch(request)).status).toBe(200);
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(sent).toEqual([
      { path: "/api/v1/posts", body: '{"text":"hi"}' },
      { path: "/api/v1/posts", body: '{"text":"hi"}' },
    ]);
    expect(onSessionLost).not.toHaveBeenCalled();
  });

  it("returns a second 401 as it is, without refreshing again", async () => {
    const { authFetch, fetch, refresh } = setup([401, 401, 200]);
    expect((await authFetch(new Request(url("/auth/me")))).status).toBe(401);
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it("does not retry and reports the lost session when refreshing fails", async () => {
    const { authFetch, fetch, onSessionLost } = setup([401], false);
    expect((await authFetch(new Request(url("/auth/me")))).status).toBe(401);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(onSessionLost).toHaveBeenCalledTimes(1);
  });

  it("treats a refresh that throws as failed", async () => {
    const onSessionLost = jest.fn();
    const authFetch = createAuthFetch({
      fetch: async () => status(401),
      refresh: () => Promise.reject(new Error("offline")),
      onSessionLost,
    });
    expect((await authFetch(new Request(url("/auth/me")))).status).toBe(401);
    expect(onSessionLost).toHaveBeenCalledTimes(1);
  });

  it("shares one refresh between requests that get 401 at the same time", async () => {
    const { authFetch, refresh } = setup([401, 401, 401, 200, 200, 200]);
    const answers = await Promise.all(["/a", "/b", "/c"].map((p) => authFetch(new Request(url(p)))));
    expect(answers.map((r) => r.status)).toEqual([200, 200, 200]);
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it.each(["/auth/login", "/auth/refresh", "/auth/logout", "/auth/register", "/auth/verify-email", "/auth/resend-verification", "/auth/password-reset/request", "/auth/password-reset/confirm"])(
    "does not refresh for %s",
    async (path) => {
      const { authFetch, fetch, refresh, onSessionLost } = setup([401]);
      expect((await authFetch(new Request(url(path), { method: "POST" }))).status).toBe(401);
      expect(refresh).not.toHaveBeenCalled();
      expect(onSessionLost).not.toHaveBeenCalled();
      expect(fetch).toHaveBeenCalledTimes(1);
    },
  );
});

describe("createSessionRefresher", () => {
  const realNavigator = Object.getOwnPropertyDescriptor(globalThis, "navigator");
  function setLocks(locks: unknown) {
    Object.defineProperty(globalThis, "navigator", { configurable: true, value: { locks } });
  }
  afterAll(() => {
    if (realNavigator) Object.defineProperty(globalThis, "navigator", realNavigator);
  });

  // fakeLocks grants a named lock to one holder at a time, as the browser does across tabs.
  function fakeLocks() {
    let tail: Promise<unknown> = Promise.resolve();
    return {
      request<T>(_name: string, run: () => Promise<T>): Promise<T> {
        const result = tail.then(run);
        tail = result.catch(() => {});
        return result;
      },
    };
  }

  it.each([
    [204, "ok"],
    [401, "refused"],
    [500, "unavailable"],
    [503, "unavailable"],
  ])("reads %i as %s", async (code, outcome) => {
    setLocks(undefined);
    expect(await createSessionRefresher(async () => code)()).toBe(outcome);
  });

  it("reads no answer as unavailable, never as refused", async () => {
    setLocks(undefined);
    const refresh = createSessionRefresher(async () => {
      throw new TypeError("offline");
    });
    expect(await refresh()).toBe("unavailable");
  });

  it("shares one request between callers in a tab", async () => {
    setLocks(undefined);
    const send = jest.fn(async () => 204);
    const refresh = createSessionRefresher(send);
    await Promise.all([refresh(), refresh(), refresh()]);
    expect(send).toHaveBeenCalledTimes(1);
    await refresh();
    expect(send).toHaveBeenCalledTimes(2);
  });

  it("never lets two tabs refresh at once, as a second use would end the session", async () => {
    setLocks(fakeLocks());
    let running = 0;
    let most = 0;
    const send = async () => {
      most = Math.max(most, ++running);
      await new Promise((resolve) => setTimeout(resolve, 1));
      running--;
      return 204;
    };
    const tabs = [createSessionRefresher(send), createSessionRefresher(send), createSessionRefresher(send)];
    expect(await Promise.all(tabs.map((refresh) => refresh()))).toEqual(["ok", "ok", "ok"]);
    expect(most).toBe(1);
  });

  it("uses one lock name in every tab", async () => {
    const request = jest.fn((_name: string, run: () => Promise<unknown>) => run());
    setLocks({ request });
    await createSessionRefresher(async () => 204)();
    await createSessionRefresher(async () => 204)();
    expect(request.mock.calls[0][0]).toBe(request.mock.calls[1][0]);
  });
});
