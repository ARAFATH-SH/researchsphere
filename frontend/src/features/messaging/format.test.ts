import { bodyLength, dayLabel, groupMessages, rowPreview, rowTime, seenBy, seenMessageId, threadStatus, type ThreadMessage, waitText } from "./format";
import { adaID, bobID, conversation, group, nafisaID, rahimID } from "./test-data";

const now = new Date(2026, 10, 5, 18, 0);

function shown(n: number, senderId: string, createdAt: string): ThreadMessage {
  return { key: `k${n}`, id: `id-${String(n).padStart(3, "0")}`, senderId, event: null, body: "x", createdAt, editedAt: null, deleted: false, attachments: [], delivery: "sent" };
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
    const mine = conversation({ lastMessage: { senderId: adaID, senderName: "Ada Lovelace", kind: "text", event: null, body: "Hello", deleted: false, attachmentCount: 0, createdAt: "2026-11-05T09:00:00Z" } });
    expect(rowPreview(mine, adaID)).toBe("You: Hello");
    expect(rowPreview(conversation({ lastMessage: null }), adaID)).toBe("");
  });

  const last = (senderId: string, body: string | null, deleted: boolean, attachmentCount: number) =>
    conversation({ lastMessage: { senderId, senderName: null, kind: "text", event: null, body, deleted, attachmentCount, createdAt: "2026-11-05T09:00:00Z" } });

  it("says Message deleted from the deleted flag, never from a missing body", () => {
    expect(rowPreview(last(bobID, null, true, 0), adaID)).toBe("Message deleted");
    expect(rowPreview(last(adaID, null, true, 0), adaID)).toBe("Message deleted");
    expect(rowPreview(last(bobID, null, false, 1), adaID)).toBe("Sent a file");
  });

  it("names files sent with no text", () => {
    expect(rowPreview(last(bobID, null, false, 3), adaID)).toBe("Sent 3 files");
    expect(rowPreview(last(adaID, "", false, 1), adaID)).toBe("You sent a file");
    expect(rowPreview(last(adaID, "Look", false, 2), adaID)).toBe("You: Look");
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

describe("group rows (FR-16.6)", () => {
  it("names the sender by first name, or You, and says a system message from its key", () => {
    expect(rowPreview(group(), adaID)).toBe("Nafisa: I'll check the references");
    const mine = group({ lastMessage: { ...group().lastMessage!, senderId: adaID, senderName: "Ada Lovelace", body: "On it" } });
    expect(rowPreview(mine, adaID)).toBe("You: On it");
    const file = group({ lastMessage: { ...group().lastMessage!, body: null, attachmentCount: 2 } });
    expect(rowPreview(file, adaID)).toBe("Nafisa sent 2 files");
    const renamed = group({ lastMessage: { ...group().lastMessage!, kind: "system", event: "group_renamed", body: null } });
    expect(rowPreview(renamed, adaID)).toBe("Nafisa renamed the group");
  });
});

describe("groupMessages and system messages", () => {
  it("ends a run at a system message, which stands alone", () => {
    const system = { ...shown(2, nafisaID, "2026-11-05T09:01:00Z"), event: { type: "member_added" as const, actorId: nafisaID, userIds: [], oldName: null, newName: null } };
    const [day] = groupMessages([shown(1, nafisaID, "2026-11-05T09:00:00Z"), system, shown(3, nafisaID, "2026-11-05T09:02:00Z")], now);
    expect(day.groups.map((g) => [g.system, g.messages.length])).toEqual([
      [false, 1],
      [true, 1],
      [false, 1],
    ]);
  });
});

describe("seenBy (FR-16.3)", () => {
  const mine = (n: number) => shown(n, adaID, "2026-11-05T09:00:00Z");
  it("finds my newest message someone has read, with everyone who has", () => {
    const messages = [mine(1), shown(2, nafisaID, "2026-11-05T09:00:00Z"), mine(3), mine(4)];
    const readers = [
      { userId: nafisaID, lastReadMessageId: "id-003" },
      { userId: rahimID, lastReadMessageId: "id-004" },
      { userId: "x", lastReadMessageId: null },
    ];
    expect(seenBy(messages, adaID, readers)).toEqual({ messageId: "id-004", userIds: [rahimID] });
    expect(seenBy(messages, adaID, [{ userId: nafisaID, lastReadMessageId: "id-003" }])).toEqual({ messageId: "id-003", userIds: [nafisaID] });
    expect(seenBy(messages, adaID, [{ userId: nafisaID, lastReadMessageId: null }])).toBeNull();
    expect(seenBy(messages, adaID, null)).toBeNull();
  });
});
