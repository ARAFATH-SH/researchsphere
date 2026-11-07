import { isStale, sendNotice } from "./send-errors";

const failure = (code: string, retryAfter?: number) => ({ ok: false as const, code, message: `server: ${code}`, retryAfter });

describe("sendNotice", () => {
  it.each([
    ["request_pending", "warning", "You can send more messages once your request is accepted."],
    ["conversation_closed", "error", "This conversation is closed."],
    ["cannot_message", "error", "You can't message this person."],
    ["not_found", "error", "You can't message this person."],
    ["you_blocked_this_user", "error", "You've blocked this person, so you can't message them."],
    ["messaging_restricted", "error", "server: messaging_restricted"],
    ["forbidden", "error", "server: forbidden"],
    ["network_error", "error", "server: network_error"],
  ])("maps %s", (code, tone, text) => {
    expect(sendNotice(failure(code))).toEqual({ tone, text });
  });

  it("says how long a rate limit lasts", () => {
    expect(sendNotice(failure("rate_limited", 30))).toEqual({
      tone: "warning",
      text: "You're sending messages too quickly. Try again in 30 seconds.",
    });
    expect(sendNotice(failure("rate_limited")).text).toContain("in a minute");
  });
});

describe("isStale", () => {
  it("is true when the conversation changed, not for a limit or a lost connection", () => {
    expect(isStale("conversation_closed")).toBe(true);
    expect(isStale("request_pending")).toBe(true);
    expect(isStale("rate_limited")).toBe(false);
    expect(isStale("network_error")).toBe(false);
  });
});
