import { act, createEvent, fireEvent, render, screen, within } from "@testing-library/react";

import { getCurrentUser } from "@/features/auth/api";
import { getPresence } from "@/features/realtime/api";
import { FakeWebSocket } from "@/features/realtime/fake-websocket";
import { startSocket, stopSocket } from "@/features/realtime/socket";
import type { UploadOptions, UploadResult } from "@/lib/api/upload";

import {
  type AttachmentUpload,
  getConversation,
  listMessages,
  markConversationRead,
  sendMessage,
  uploadAttachment,
} from "./api";
import { adaID, conversation, conversationID, message } from "./test-data";
import { Thread } from "./thread";
import { refreshUnreadMessages } from "./unread-count";
import { ConversationsProvider } from "./use-conversations";

jest.mock("./api");
jest.mock("./unread-count");
jest.mock("@/features/auth/api");
jest.mock("@/features/realtime/api", () => ({ getPresence: jest.fn(), maxPresenceIds: 50 }));
jest.mock("@/features/auth/session", () => ({
  useSession: () => ({ status: "signedIn", user: { id: "0b8f4c1e-0000-4000-8000-000000000001" } }),
}));

const mockUpload = jest.mocked(uploadAttachment);
const mockSend = jest.mocked(sendMessage);

const pdf = (name = "draft.pdf", size = 2048) => new File([new Uint8Array(size)], name, { type: "application/pdf" });
const png = (name = "figure.png") => new File([new Uint8Array(10)], name, { type: "image/png" });

function record(id: string, name: string, status: AttachmentUpload["status"] = "pending"): AttachmentUpload {
  return { id, name, size: 2048, contentType: "application/pdf", status, expiresAt: "2026-11-11T09:00:00Z" };
}

// Each upload waits until the test finishes it, and records its progress callback and signal.
type Pending = { file: File; options: UploadOptions; finish: (res: UploadResult<AttachmentUpload>) => void };
let uploads: Pending[];

async function renderThread() {
  await act(async () => FakeWebSocket.latest().accept());
  render(
    <ConversationsProvider>
      <Thread conversationId={conversationID} />
    </ConversationsProvider>,
  );
  await screen.findByRole("log");
}

const box = () => screen.getByRole("textbox", { name: "Write a message" }) as HTMLTextAreaElement;
const sendButton = () => screen.getByRole("button", { name: "Send" }) as HTMLButtonElement;
const chips = () => within(screen.getByRole("list", { name: "Files to send" }));
const pick = (...files: File[]) => act(async () => fireEvent.change(screen.getByTestId("attach-input"), { target: { files } }));
const finish = (i: number, res: UploadResult<AttachmentUpload>) => act(async () => uploads[i].finish(res));

beforeEach(() => {
  uploads = [];
  jest.mocked(getCurrentUser).mockResolvedValue({ ok: false, code: "network_error", message: "No." });
  jest.spyOn(document, "hasFocus").mockImplementation(() => true);
  jest.mocked(markConversationRead).mockResolvedValue({ ok: true, data: undefined });
  jest.mocked(refreshUnreadMessages).mockResolvedValue();
  jest.mocked(getPresence).mockResolvedValue({ ok: true, data: [] });
  jest.mocked(getConversation).mockResolvedValue({ ok: true, data: conversation() });
  jest.mocked(listMessages).mockResolvedValue({ ok: true, data: { items: [], nextBefore: null, otherLastReadMessageId: null, readers: null } });
  mockUpload.mockImplementation(
    (_id, file, options = {}) =>
      new Promise((resolve) => {
        options.signal?.addEventListener("abort", () => resolve({ ok: false, status: 0, code: "aborted", message: "Cancelled." }));
        uploads.push({ file, options, finish: resolve });
      }),
  );
  Object.defineProperty(globalThis.crypto, "randomUUID", { configurable: true, value: jest.fn(() => "client-1") });
  FakeWebSocket.install();
  startSocket({ url: "ws://test", refresh: jest.fn().mockResolvedValue("ok"), onSessionLost: jest.fn() });
});
afterEach(() => {
  stopSocket();
  jest.resetAllMocks();
});

describe("Adding files", () => {
  it("has the attach button of the design, which opens a file picker for several files", async () => {
    await renderThread();
    const input = screen.getByTestId("attach-input") as HTMLInputElement;
    const click = jest.spyOn(input, "click");
    fireEvent.click(screen.getByRole("button", { name: "Attach file (PDF, image, office document up to 25 MB)" }));
    expect(click).toHaveBeenCalled();
    expect(input.multiple).toBe(true);
    expect(input.accept).toContain(".docx");
  });

  it("uploads each picked file at once, with a labelled progress bar", async () => {
    await renderThread();
    await pick(pdf("a.pdf"), pdf("b.pdf"));
    expect(mockUpload).toHaveBeenCalledTimes(2);
    expect(mockUpload.mock.calls[0][0]).toBe(conversationID);
    act(() => uploads[0].options.onProgress?.(0.42));
    const bar = screen.getByRole("progressbar", { name: "Uploading a.pdf" }) as HTMLProgressElement;
    expect(bar.value).toBe(0.42);
    expect(chips().getByText("Uploading… 42%")).toBeTruthy();
  });

  it("attaches files dropped on the thread, with a drop hint while dragging", async () => {
    await renderThread();
    const thread = screen.getByRole("region", { name: "Conversation with Bob Babbage" });
    const dataTransfer = { types: ["Files"], files: [pdf("dropped.pdf")], dropEffect: "none" };
    fireEvent.dragEnter(thread, { dataTransfer });
    expect(screen.getByText("Drop files to attach")).toBeTruthy();
    await act(async () => fireEvent.drop(thread, { dataTransfer }));
    expect(screen.queryByText("Drop files to attach")).toBeNull();
    expect(mockUpload.mock.calls[0][1].name).toBe("dropped.pdf");
  });

  it("attaches pasted images and leaves other pastes alone", async () => {
    await renderThread();
    const paste = (files: File[], text = "") => {
      const event = createEvent.paste(box(), { clipboardData: { files, getData: () => text } });
      fireEvent(box(), event);
      return event;
    };
    let event = paste([png("shot.png")]);
    expect(event.defaultPrevented).toBe(true);
    expect(mockUpload.mock.calls[0][1].name).toBe("shot.png");
    event = paste([], "just text");
    expect(event.defaultPrevented).toBe(false);
    paste([pdf("not-an-image.pdf")]);
    expect(mockUpload).toHaveBeenCalledTimes(1);
  });

  it("refuses a type the server does not take, a file over 25 MB and a sixth file, saying why", async () => {
    await renderThread();
    const big = pdf("huge.pdf", 10);
    Object.defineProperty(big, "size", { value: 25 * 1024 * 1024 + 1 });
    await pick(new File(["x"], "macro.docm", { type: "application/vnd.ms-word.document.macroEnabled.12" }), big);
    expect(mockUpload).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toBe(
      "macro.docm can't be attached. Use a PDF, PNG, JPEG, WebP, DOCX, XLSX or PPTX file. huge.pdf is over 25 MB.",
    );
    await pick(pdf("1.pdf"), pdf("2.pdf"), pdf("3.pdf"), pdf("4.pdf"), pdf("5.pdf"), pdf("6.pdf"));
    expect(mockUpload).toHaveBeenCalledTimes(5);
    expect(screen.getByRole("alert").textContent).toBe("You can attach up to 5 files to a message.");
  });

  it("takes an Office file the browser gave no type, by its extension", async () => {
    await renderThread();
    await pick(new File(["x"], "notes.docx", { type: "" }));
    expect(mockUpload).toHaveBeenCalledTimes(1);
  });

  it("removes a file, cancelling its upload", async () => {
    await renderThread();
    await pick(pdf("a.pdf"));
    await act(async () => fireEvent.click(chips().getByRole("button", { name: "Remove a.pdf" })));
    expect(uploads[0].options.signal?.aborted).toBe(true);
    expect(screen.queryByRole("list", { name: "Files to send" })).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it.each([
    [{ status: 413, code: "file_too_large", message: "Too large." }, "This file is over 25 MB."],
    [{ status: 415, code: "unsupported_type", message: "No." }, "This type of file can't be sent. Use a PDF, PNG, JPEG, WebP, DOCX, XLSX or PPTX file."],
    [{ status: 429, code: "rate_limited", message: "Slow.", retryAfter: 600 }, "You've reached the upload limit. Try again in 10 minutes."],
  ])("shows a refused upload on its chip (%o) and blocks Send until it is removed", async (failure, text) => {
    await renderThread();
    await pick(pdf("a.pdf"));
    await finish(0, { ok: false, ...failure });
    expect(chips().getByText(text)).toBeTruthy();
    fireEvent.change(box(), { target: { value: "hello" } });
    expect(sendButton().disabled).toBe(true);
    expect(screen.getByText("Remove the files that can't be sent.")).toBeTruthy();
    fireEvent.click(chips().getByRole("button", { name: "Remove a.pdf" }));
    expect(sendButton().disabled).toBe(false);
  });
});

describe("Sending files", () => {
  it("waits while a file uploads, then sends with no text", async () => {
    await renderThread();
    await pick(pdf("a.pdf"));
    expect(sendButton().disabled).toBe(true);
    expect(screen.getByText("Waiting for files to finish uploading…").getAttribute("role")).toBe("status");
    expect(sendButton().getAttribute("aria-describedby")).toBeTruthy();
    await finish(0, { ok: true, data: record("f1", "a.pdf") });
    expect(chips().getByText("2 KB")).toBeTruthy();
    expect(sendButton().disabled).toBe(false);

    let done: (value: Awaited<ReturnType<typeof sendMessage>>) => void = () => {};
    mockSend.mockReturnValue(new Promise((resolve) => (done = resolve)));
    await act(async () => fireEvent.click(sendButton()));
    expect(mockSend).toHaveBeenCalledWith(conversationID, "", "client-1", ["f1"]);
    expect(screen.queryByRole("list", { name: "Files to send" })).toBeNull();
    // The message on its way shows its file.
    expect(within(screen.getByRole("log")).getByText("a.pdf")).toBeTruthy();
    const sent = { ...message(1, adaID, ""), body: null, attachments: [{ id: "f1", name: "a.pdf", size: 2048, contentType: "application/pdf", status: "pending" as const }] };
    await act(async () => done({ ok: true, data: { message: sent, conversationState: "active" } }));
    expect(within(screen.getByRole("log")).getByText("Scanning for viruses…")).toBeTruthy();
  });

  it("sends text and files together", async () => {
    mockSend.mockReturnValue(new Promise(() => {}));
    await renderThread();
    await pick(pdf("a.pdf"), png("b.png"));
    await finish(0, { ok: true, data: record("f1", "a.pdf") });
    await finish(1, { ok: true, data: record("f2", "b.png") });
    fireEvent.change(box(), { target: { value: " see these " } });
    await act(async () => fireEvent.keyDown(box(), { key: "Enter" }));
    expect(mockSend).toHaveBeenCalledWith(conversationID, "see these", "client-1", ["f1", "f2"]);
  });

  it("still needs text or a file", async () => {
    await renderThread();
    expect(sendButton().disabled).toBe(true);
  });

  it("shows the scan result on the chip from the socket, and blocks Send for a blocked file", async () => {
    await renderThread();
    await pick(pdf("a.pdf"));
    // The result may come before the upload's answer; it waits for it.
    await act(async () =>
      FakeWebSocket.latest().receive("attachment.updated", { conversationId: conversationID, messageId: null, attachmentId: "f1", status: "infected" }),
    );
    await finish(0, { ok: true, data: record("f1", "a.pdf") });
    expect(chips().getByText("This file failed a security check. Remove it to send.")).toBeTruthy();
    expect(sendButton().disabled).toBe(true);

    await pick(pdf("b.pdf"));
    await finish(1, { ok: true, data: record("f2", "b.pdf") });
    await act(async () =>
      FakeWebSocket.latest().receive("attachment.updated", { conversationId: conversationID, messageId: null, attachmentId: "f2", status: "failed" }),
    );
    expect(chips().getByText("This file couldn't be checked. Remove it to send.")).toBeTruthy();
  });
});
