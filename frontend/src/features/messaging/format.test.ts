import { bodyLength, dayLabel, groupMessages, rowPreview, rowTime, seenMessageId, threadStatus, type ThreadMessage, waitText } from "./format";
import { adaID, bobID, conversation } from "./test-data";

const now = new Date(2026, 10, 5, 18, 0);

function shown(n: number, senderId: string, createdAt: string): ThreadMessage {
  return { key: `k${n}`, id: `id-${String(n).padStart(3, "0")}`, senderId, body: "x", createdAt, delivery: "sent" };
}

describe("bodyLength", () => {
  it("trims and counts code points, as the server does", () => {
    expect(bodyLength("  ab \n")).toBe(2);
    expect(bodyLength("😀😀")).toBe(2);
  });
});

describe("rowTime", () => {
  it.each([
    [new Date(2026, 10, 5, 9, 5), "9:05 AM"],
    [new Date(2026, 10, 2, 9, 5), "Mon"],
    [new Date(2026, 9, 3, 9, 5), "3 Oct"],
    [new Date(2025, 9, 3, 9, 5), "3 Oct 2025"],
  ])("shows %s as %s", (date, want) => {
    expect(rowTime(date.toISOString(), now).replace(/ /g, " ")).toBe(want);
  });
});

describe("dayLabel", () => {
  it.each([
    [new Date(2026, 10, 5, 1), "Today"],
    [new Date(2026, 10, 4, 23), "Yesterday"],
    [new Date(2026, 10, 1, 12), "1 Nov 2026"],
  ])("shows %s as %s", (date, want) => {
    expect(dayLabel(date.toISOString(), now)).toBe(want);
  });
});

describe("waitText", () => {
  it.each([
    [undefined, "in a minute"],
    [1, "in 1 second"],
    [30, "in 30 seconds"],
    [60, "in a minute"],
    [90, "in 2 minutes"],
    [7200, "in 2 hours"],
  ])("says %s seconds as %s", (seconds, want) => {
    expect(waitText(seconds)).toBe(want);
  });
});

describe("rowPreview", () => {
  it("shows the last message, with You: before my own", () => {
    expect(rowPreview(conversation(), adaID)).toBe("See you");
    const mine = conversation({ lastMessage: { senderId: adaID, kind: "text", event: null, body: "Hello", deleted: false, createdAt: "2026-11-05T09:00:00Z" } });
    expect(rowPreview(mine, adaID)).toBe("You: Hello");
    expect(rowPreview(conversation({ lastMessage: null }), adaID)).toBe("");
  });

  it("says who is waiting on a request", () => {
    expect(rowPreview(conversation({ state: "request", requestedBy: adaID }), adaID)).toBe("Request sent");
    expect(rowPreview(conversation({ state: "request", requestedBy: bobID }), adaID)).toBe("Wants to message you");
  });
});

describe("threadStatus", () => {
  it.each([
    [conversation(), "open"],
    [conversation({ state: "request", requestedBy: adaID, canMessage: false }), "waiting"],
    [conversation({ state: "request", requestedBy: bobID, canMessage: false }), "received"],
    [conversation({ state: "declined", requestedBy: adaID, canMessage: false }), "closed"],
    [conversation({ canMessage: false }), "readOnly"],
  ])("reads %#", (c, want) => {
    expect(threadStatus(c, adaID)).toBe(want);
  });
});

describe("groupMessages", () => {
  const today = new Date(2026, 10, 5, 9).toISOString();
  const earlier = new Date(2026, 10, 4, 9).toISOString();

  it("groups by day, then by runs of one sender", () => {
    const days = groupMessages(
      [shown(1, bobID, earlier), shown(2, bobID, today), shown(3, bobID, today), shown(4, adaID, today), shown(5, bobID, today)],
      now,
    );
    expect(days.map((d) => d.label)).toEqual(["Yesterday", "Today"]);
    expect(days[1].groups.map((g) => [g.senderId, g.messages.length])).toEqual([
      [bobID, 2],
      [adaID, 1],
      [bobID, 1],
    ]);
  });

  it("starts a new group on a new day, even for the same sender", () => {
    const days = groupMessages([shown(1, bobID, earlier), shown(2, bobID, today)], now);
    expect(days.flatMap((d) => d.groups)).toHaveLength(2);
  });
});

describe("seenMessageId", () => {
  const at = "2026-11-05T09:00:00Z";
  const messages = [shown(1, adaID, at), shown(2, adaID, at), shown(3, bobID, at), shown(4, adaID, at)];

  it("is my latest message at or before their marker", () => {
    expect(seenMessageId(messages, adaID, "id-003", conversation())).toBe("id-002");
    expect(seenMessageId(messages, adaID, "id-004", conversation())).toBe("id-004");
  });

  it("is nothing without a marker or before my first message", () => {
    expect(seenMessageId(messages, adaID, null, conversation())).toBeNull();
    expect(seenMessageId(messages, adaID, "id-000", conversation())).toBeNull();
  });

  it("skips a message still on its way", () => {
    const sending: ThreadMessage = { ...shown(5, adaID, at), id: null, delivery: "sending" };
    expect(seenMessageId([...messages, sending], adaID, "id-999", conversation())).toBe("id-004");
  });

  it.each([
    ["a pending request", conversation({ state: "request", requestedBy: adaID, canMessage: false })],
    ["a declined request", conversation({ state: "declined", requestedBy: adaID, canMessage: false })],
  ])("is nothing on %s, even with a marker", (_, c) => {
    expect(seenMessageId(messages, adaID, "id-004", c)).toBeNull();
  });
});
