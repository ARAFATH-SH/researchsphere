import { render, screen } from "@testing-library/react";

import { emptyProfile } from "@/features/profile/test-profiles";

import { MessageButton, messageAction } from "./message-button";
import { adaID, bobID, conversationID } from "./test-data";

const bob = (overrides = {}) => ({ ...emptyProfile(bobID, "Bob Babbage"), ...overrides });
const chat = (state: "active" | "request" | "declined", canMessage: boolean, requestedBy: string | null = null) => ({
  id: conversationID,
  state,
  canMessage,
  requestedBy,
});
const newHref = `/messaging/new?to=${bobID}`;
const openHref = `/messaging/${conversationID}`;

describe("messageAction", () => {
  it.each([
    ["no conversation, connected", bob(), true, { label: "Message", href: newHref, kind: "filled" }],
    ["no conversation, not connected", bob(), false, { label: "Message request", href: newHref, kind: "outline" }],
    ["active", bob({ conversation: chat("active", true) }), true, { label: "Message", href: openHref, kind: "filled" }],
    ["active after a disconnect", bob({ conversation: chat("active", true) }), false, { label: "Message", href: openHref, kind: "filled" }],
    ["request I sent", bob({ conversation: chat("request", false, adaID) }), false, { label: "Message request sent", href: openHref, kind: "outline" }],
    ["request to me", bob({ conversation: chat("request", false, bobID) }), false, { label: "Reply to request", href: openHref, kind: "outline" }],
  ])("%s", (_, profile, connected, want) => {
    expect(messageAction(profile, connected, adaID)).toEqual(want);
  });

  it.each([
    ["declined", bob({ conversation: chat("declined", false, adaID) }), adaID],
    ["cannot message", bob({ conversation: chat("active", false) }), adaID],
    ["blocked by me, with no conversation", bob({ blockedByMe: true }), adaID],
    ["blocked by me, with a request", bob({ blockedByMe: true, conversation: chat("request", false, adaID) }), adaID],
    ["my own profile", bob(), bobID],
  ])("has no button when %s", (_, profile, me) => {
    expect(messageAction(profile, true, me)).toBeNull();
  });
});

describe("MessageButton", () => {
  it("is a link styled as the profile's pill", () => {
    render(<MessageButton action={{ label: "Message", href: openHref, kind: "filled" }} />);
    const link = screen.getByRole("link", { name: "Message" });
    expect(link.getAttribute("href")).toBe(openHref);
    expect(link.className).toContain("h-10");
  });
});
