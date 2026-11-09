import { backoffDelay, closeAction, maxDelay } from "./close-policy";
import { parseEvent } from "./events";
import { FakeWebSocket } from "./fake-websocket";
import { onConnected, onRealtimeEvent, sendEvent, socketStatus, stableAfter, startSocket, stopSocket } from "./socket";

const url = "ws://test/api/v1/ws";
const refresh = jest.fn();
const onSessionLost = jest.fn();

let visibility: DocumentVisibilityState = "visible";
Object.defineProperty(document, "visibilityState", { configurable: true, get: () => visibility });

function setVisibility(next: DocumentVisibilityState) {
  visibility = next;
  document.dispatchEvent(new Event("visibilitychange"));
}

const sockets = () => FakeWebSocket.instances.length;
const latest = () => FakeWebSocket.latest();

// connect starts the socket and lets the server accept it.
function connect() {
  startSocket({ url, refresh, onSessionLost });
  latest().accept();
}

// wait lets a refresh answer and timers run; the most a backoff at attempt 0 can take is 1 s.
async function wait(ms: number) {
  await jest.advanceTimersByTimeAsync(ms);
}

beforeEach(() => {
  jest.useFakeTimers();
  FakeWebSocket.install();
  visibility = "visible";
  refresh.mockResolvedValue("ok");
  // No jitter off the top: every delay is its full length.
  jest.spyOn(Math, "random").mockReturnValue(1);
});
afterEach(() => {
  stopSocket();
  jest.useRealTimers();
  jest.resetAllMocks();
  jest.restoreAllMocks();
});

describe("backoffDelay", () => {
  it("doubles from 1 s up to 30 s", () => {
    expect([0, 1, 2, 3, 4, 5, 6, 20].map((n) => backoffDelay(n, 1))).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000]);
  });

  it("takes up to half off as jitter", () => {
    expect(backoffDelay(3, 0)).toBe(4000);
    expect(backoffDelay(3, 0.5)).toBe(6000);
    expect(backoffDelay(30, 0)).toBe(maxDelay / 2);
  });
});

describe("closeAction", () => {
  it.each([
    [4401, "refresh"],
    [4403, "refresh"],
    [4409, "replaced"],
    [1001, "retry"],
    [1009, "retry"],
    [1013, "retry"],
    [1006, "retry"],
  ])("answers an open socket's close %i with %s", (code, action) => {
    expect(closeAction(code, true)).toBe(action);
  });

  it("treats any close before opening as a refused handshake", () => {
    expect(closeAction(1006, false)).toBe("handshake");
    expect(closeAction(4401, false)).toBe("handshake");
  });
});

describe("parseEvent", () => {
  it("reads a known event", () => {
    expect(parseEvent('{"type":"presence","data":{"userId":"u","online":true}}')).toEqual({ type: "presence", data: { userId: "u", online: true } });
  });

  it.each([["not json"], ['{"type":"later.event","data":{}}'], ['{"type":"presence"}'], ["null"], [new ArrayBuffer(1)]])("ignores %s", (frame) => {
    expect(parseEvent(frame)).toBeNull();
  });

  // Group events wait for the group screens of slice 14B.5.
  it("ignores conversation.members and a group's access-lost update", () => {
    expect(parseEvent('{"type":"conversation.members","data":{"conversationId":"g","change":"renamed","actorId":"u","userIds":[],"name":"Lab"}}')).toBeNull();
    expect(parseEvent('{"type":"conversation.updated","data":{"conversationId":"g","state":"active","canMessage":false,"accessLost":true}}')).toBeNull();
    expect(parseEvent('{"type":"conversation.updated","data":{"conversationId":"c","state":"declined","canMessage":false,"accessLost":false}}')).not.toBeNull();
  });

  // Attachment scan results (FR-16.7) wait for the attachment UI of slice 14B.5.
  it("ignores attachment.updated", () => {
    expect(parseEvent('{"type":"attachment.updated","data":{"conversationId":"c","messageId":null,"attachmentId":"a","status":"clean"}}')).toBeNull();
  });
});

describe("the connection", () => {
  it("opens one socket on the given URL", () => {
    connect();
    expect(sockets()).toBe(1);
    expect(latest().url).toBe(url);
    expect(socketStatus()).toBe("open");
  });

  it("passes events on and drops frames it cannot read", () => {
    const heard = jest.fn();
    const stop = onRealtimeEvent(heard);
    connect();
    latest().receive("unread.changed", { total: 1, requests: 0 });
    latest().receive("later.event", {});
    latest().onmessage?.({ data: "not json" });
    stop();
    expect(heard).toHaveBeenCalledTimes(1);
    expect(heard).toHaveBeenCalledWith({ type: "unread.changed", data: { total: 1, requests: 0 } });
  });

  it("sends a frame only while open", () => {
    startSocket({ url, refresh, onSessionLost });
    expect(sendEvent("typing.start", "c")).toBe(false);
    latest().accept();
    expect(sendEvent("typing.start", "c")).toBe(true);
    expect(latest().sent).toEqual([{ type: "typing.start", data: { conversationId: "c" } }]);
    latest().serverClose(1013);
    expect(sendEvent("typing.stop", "c")).toBe(false);
  });

  it("tells listeners after every connect and reconnect", async () => {
    const connected = jest.fn();
    const stop = onConnected(connected);
    connect();
    expect(connected).toHaveBeenCalledTimes(1);
    latest().serverClose(1001);
    await wait(1000);
    latest().accept();
    stop();
    expect(connected).toHaveBeenCalledTimes(2);
  });
});

describe("backoff", () => {
  it("waits longer after each failure, up to 30 s", async () => {
    connect();
    latest().serverClose(1001);
    expect(socketStatus()).toBe("waiting");
    for (const delay of [1000, 2000, 4000, 8000, 16000, 30000, 30000]) {
      const before = sockets();
      await wait(delay - 1);
      expect(sockets()).toBe(before);
      await wait(1);
      expect(sockets()).toBe(before + 1);
      latest().serverClose(1006); // never opens
    }
  });

  it("starts again from 1 s after a stable connection", async () => {
    connect();
    latest().serverClose(1001);
    await wait(1000);
    latest().serverClose(1006);
    await wait(2000);
    latest().accept();
    await wait(stableAfter);
    const before = sockets();
    latest().serverClose(1001);
    await wait(1000);
    expect(sockets()).toBe(before + 1);
  });

  it("keeps backing off when connections do not last", async () => {
    connect();
    latest().serverClose(1001);
    await wait(1000);
    latest().accept();
    await wait(stableAfter - 1);
    const before = sockets();
    latest().serverClose(1001);
    await wait(1999);
    expect(sockets()).toBe(before);
    await wait(1);
    expect(sockets()).toBe(before + 1);
  });
});

describe("close codes", () => {
  it.each([1001, 1013])("reconnects after %i with no refresh", async (code) => {
    connect();
    latest().serverClose(code);
    await wait(1000);
    expect(sockets()).toBe(2);
    expect(refresh).not.toHaveBeenCalled();
  });

  it("logs 1009 without any content, and reconnects", async () => {
    const warn = jest.spyOn(console, "warn").mockImplementation(() => {});
    connect();
    latest().serverClose(1009);
    expect(warn).toHaveBeenCalledTimes(1);
    await wait(1000);
    expect(sockets()).toBe(2);
  });

  it("4401 refreshes, then reconnects", async () => {
    connect();
    latest().serverClose(4401);
    expect(refresh).toHaveBeenCalledTimes(1);
    await wait(1000);
    expect(sockets()).toBe(2);
    expect(onSessionLost).not.toHaveBeenCalled();
  });

  it("does not reconnect before the refresh has answered", async () => {
    let answer: (outcome: string) => void = () => {};
    refresh.mockReturnValue(new Promise((resolve) => (answer = resolve)));
    connect();
    latest().serverClose(4401);
    await wait(60_000);
    expect(sockets()).toBe(1);
    answer("ok");
    await wait(1000);
    expect(sockets()).toBe(2);
  });

  it.each([4401, 4403])("%i signs out when the refresh is refused, and stays away", async (code) => {
    refresh.mockResolvedValue("refused");
    connect();
    latest().serverClose(code);
    await wait(60_000);
    expect(onSessionLost).toHaveBeenCalledTimes(1);
    expect(sockets()).toBe(1);
    expect(socketStatus()).toBe("idle");
  });

  it("4403 reconnects when the refresh works: this session is still good", async () => {
    connect();
    latest().serverClose(4403);
    await wait(1000);
    expect(sockets()).toBe(2);
    expect(onSessionLost).not.toHaveBeenCalled();
  });

  it("4403 does not sign out when the refresh gets no answer", async () => {
    refresh.mockResolvedValue("unavailable");
    connect();
    latest().serverClose(4403);
    await wait(1000);
    expect(onSessionLost).not.toHaveBeenCalled();
    expect(sockets()).toBe(2);
  });

  it("4409 stays away until the tab is shown again", async () => {
    connect();
    setVisibility("hidden");
    latest().serverClose(4409);
    expect(socketStatus()).toBe("replaced");
    await wait(10 * 60_000);
    expect(sockets()).toBe(1);
    setVisibility("hidden");
    expect(sockets()).toBe(1);
    setVisibility("visible");
    expect(sockets()).toBe(2);
    expect(refresh).not.toHaveBeenCalled();
  });

  it("does not answer a visible tab that is not replaced", async () => {
    connect();
    setVisibility("hidden");
    setVisibility("visible");
    expect(sockets()).toBe(1);
  });
});

describe("a refused handshake", () => {
  it("refreshes once per run of failures, then only backs off", async () => {
    startSocket({ url, refresh, onSessionLost });
    latest().serverClose(1006);
    await wait(1000);
    latest().serverClose(1006);
    await wait(2000);
    latest().serverClose(1006);
    await wait(4000);
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(sockets()).toBe(4);
  });

  it("refreshes again after a connection has opened", async () => {
    connect();
    latest().serverClose(1006);
    await wait(1000);
    latest().serverClose(1006);
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("signs out when the refresh is refused, and goes on when it gets no answer", async () => {
    refresh.mockResolvedValue("unavailable");
    startSocket({ url, refresh, onSessionLost });
    latest().serverClose(1006);
    await wait(1000);
    expect(onSessionLost).not.toHaveBeenCalled();
    expect(sockets()).toBe(2);
    stopSocket();

    refresh.mockResolvedValue("refused");
    startSocket({ url, refresh, onSessionLost });
    latest().serverClose(1006);
    await wait(0);
    expect(onSessionLost).toHaveBeenCalledTimes(1);
  });
});

describe("stopSocket", () => {
  it("closes the socket", () => {
    connect();
    const socket = latest();
    stopSocket();
    expect(socket.clientClose).toBe(1000);
    expect(socketStatus()).toBe("idle");
  });

  it("cancels a waiting reconnect", async () => {
    connect();
    latest().serverClose(1001);
    stopSocket();
    await wait(60_000);
    expect(sockets()).toBe(1);
  });

  it("does not reconnect or sign out when a refresh answers afterwards", async () => {
    let answer: (outcome: string) => void = () => {};
    refresh.mockReturnValue(new Promise((resolve) => (answer = resolve)));
    connect();
    latest().serverClose(4403);
    stopSocket();
    answer("refused");
    await wait(60_000);
    expect(sockets()).toBe(1);
    expect(onSessionLost).not.toHaveBeenCalled();
  });

  it("does not come back when a replaced tab is shown again", () => {
    connect();
    latest().serverClose(4409);
    stopSocket();
    setVisibility("hidden");
    setVisibility("visible");
    expect(sockets()).toBe(1);
  });

  it("ignores anything a closed socket still delivers", () => {
    const heard = jest.fn();
    const stop = onRealtimeEvent(heard);
    connect();
    const old = latest();
    const { onmessage } = old;
    stopSocket();
    onmessage?.({ data: '{"type":"unread.changed","data":{"total":1,"requests":0}}' });
    stop();
    expect(heard).not.toHaveBeenCalled();
  });
});
