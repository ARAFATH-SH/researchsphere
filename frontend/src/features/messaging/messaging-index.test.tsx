import { render, screen, waitFor } from "@testing-library/react";
import { useEffect } from "react";

import { listConversations } from "./api";
import { MessagingIndex } from "./messaging-index";
import { conversation, conversationID } from "./test-data";
import { ConversationsProvider, useConversations } from "./use-conversations";

const mockReplace = jest.fn();
jest.mock("./api");
jest.mock("@/features/auth/session", () => ({ useSession: () => ({ status: "signedIn", user: { id: "me" } }) }));
jest.mock("next/navigation", () => ({ useRouter: () => ({ replace: mockReplace }) }));

function OpenInbox() {
  const { open } = useConversations();
  useEffect(() => open("inbox"), [open]);
  return null;
}

function viewport(matches: boolean) {
  window.matchMedia = jest.fn(() => ({ matches, addEventListener: jest.fn(), removeEventListener: jest.fn() })) as unknown as typeof window.matchMedia;
}

const renderIndex = () =>
  render(
    <ConversationsProvider>
      <OpenInbox />
      <MessagingIndex />
    </ConversationsProvider>,
  );

afterEach(() => jest.resetAllMocks());

describe("MessagingIndex", () => {
  it("opens the latest conversation on desktop", async () => {
    viewport(true);
    jest.mocked(listConversations).mockResolvedValue({ ok: true, data: { items: [conversation()], nextCursor: null } });
    renderIndex();
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith(`/messaging/${conversationID}`));
  });

  it("opens nothing under 640px, so nothing is marked as read", async () => {
    viewport(false);
    const list = jest.mocked(listConversations).mockResolvedValue({ ok: true, data: { items: [conversation()], nextCursor: null } });
    renderIndex();
    await waitFor(() => expect(list).toHaveBeenCalled());
    await screen.findByText(/Select a conversation/);
    expect(mockReplace).not.toHaveBeenCalled();
  });

  it("stays put with an empty inbox", async () => {
    viewport(true);
    jest.mocked(listConversations).mockResolvedValue({ ok: true, data: { items: [], nextCursor: null } });
    renderIndex();
    await screen.findByText(/Select a conversation/);
    expect(mockReplace).not.toHaveBeenCalled();
  });
});
