import { act, fireEvent, render, screen } from "@testing-library/react";

import { useSession } from "@/features/auth/session";
import { refreshBadges } from "@/features/notification/badges";

import {
  acceptConnectionRequest,
  cancelConnectionRequest,
  type Connection,
  type ConnectionRequest,
  rejectConnectionRequest,
  sendConnectionRequest,
  type SendResult,
} from "./api";
import { ConnectButton, rateLimitedMessage } from "./connect-button";
jest.mock("./api");
jest.mock("@/features/notification/badges");
jest.mock("@/features/auth/session");

const adaID = "0b8f4c1e-0000-4000-8000-000000000001";
const bobID = "0b8f4c1e-0000-4000-8000-000000000002";
const requestID = "7a1d2c3e-0000-4000-8000-0000000000aa";
const theirRequestID = "7a1d2c3e-0000-4000-8000-0000000000bb";

const mockSend = jest.mocked(sendConnectionRequest);
const mockCancel = jest.mocked(cancelConnectionRequest);
const mockAccept = jest.mocked(acceptConnectionRequest);
const mockReject = jest.mocked(rejectConnectionRequest);
const mockRefresh = jest.mocked(refreshBadges);

beforeEach(() => {
  jest.mocked(useSession).mockReturnValue({
    status: "signedIn",
    user: { id: adaID, email: "ada@example.org", fullName: "Ada Lovelace", role: "researcher" },
  });
});
afterEach(() => jest.resetAllMocks());

// deferred is a promise the test settles, to see the optimistic state before the answer.
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => (resolve = r));
  return { promise, resolve };
}

const sent: ConnectionRequest = {
  requestId: requestID,
  createdAt: "2026-11-01T10:00:00Z",
  userId: bobID,
  fullName: "Bob Babbage",
  headline: null,
  institution: null,
  photoUrl: null,
};

const connection: Connection = {
  connectedAt: "2026-11-01T10:00:00Z",
  userId: bobID,
  fullName: "Bob Babbage",
  headline: null,
  institution: null,
  photoUrl: null,
  connectionStatus: "connected",
  connectionRequestId: null,
};

const button = (name: string) => screen.getByRole("button", { name });
const noButton = (name: string) => expect(screen.queryByRole("button", { name })).toBeNull();

function renderButton(props: Partial<Parameters<typeof ConnectButton>[0]> = {}) {
  const onChange = jest.fn();
  const view = render(
    <ConnectButton
      userId={bobID}
      name="Bob Babbage"
      status="none"
      requestId={null}
      size="profile"
      onChange={onChange}
      {...props}
    />,
  );
  return { onChange, ...view };
}

describe("ConnectButton", () => {
  it("connects: Pending shows at once, then keeps the new request's ID", async () => {
    const reply = deferred<SendResult>();
    mockSend.mockReturnValue(reply.promise);
    const { onChange } = renderButton();
    expect(button("Connect with Bob Babbage").textContent).toBe("Connect");

    fireEvent.click(button("Connect with Bob Babbage"));
    expect(button("Pending request to Bob Babbage")).toBeTruthy();
    expect(mockSend).toHaveBeenCalledWith(bobID);
    expect(onChange).toHaveBeenLastCalledWith({ status: "pending_sent", requestId: null });

    await act(async () => reply.resolve({ ok: true, data: sent }));
    expect(onChange).toHaveBeenLastCalledWith({ status: "pending_sent", requestId: requestID });
    expect(button("Pending request to Bob Babbage").hasAttribute("disabled")).toBe(false);
  });

  it("goes back to Connect with a retry message when sending fails", async () => {
    mockSend.mockResolvedValue({ ok: false, code: "internal_error", message: "Oops." });
    const { onChange } = renderButton();
    await act(async () => fireEvent.click(button("Connect with Bob Babbage")));

    expect(button("Connect with Bob Babbage")).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toBe("Couldn't send the request. Try again.");
    expect(onChange).toHaveBeenLastCalledWith({ status: "none", requestId: null });
  });

  it("says when the daily limit is reached (429)", async () => {
    mockSend.mockResolvedValue({ ok: false, code: "rate_limited", message: "Too many." });
    renderButton();
    await act(async () => fireEvent.click(button("Connect with Bob Babbage")));

    expect(button("Connect with Bob Babbage")).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toBe(rateLimitedMessage);
  });

  it("switches to Accept and Ignore when they already sent a request (409)", async () => {
    mockSend.mockResolvedValue({
      ok: false,
      code: "request_pending_from_them",
      message: "They asked first.",
      requestId: theirRequestID,
    });
    mockAccept.mockResolvedValue({ ok: true, data: connection });
    const { onChange } = renderButton();
    await act(async () => fireEvent.click(button("Connect with Bob Babbage")));

    expect(onChange).toHaveBeenLastCalledWith({ status: "pending_received", requestId: theirRequestID });
    expect(screen.getByRole("status").textContent).toBe("Bob Babbage has already sent you a request.");
    expect(button("Ignore request from Bob Babbage")).toBeTruthy();

    await act(async () => fireEvent.click(button("Accept request from Bob Babbage")));
    expect(mockAccept).toHaveBeenCalledWith(theirRequestID);
  });

  it("shows nothing when the answer is that they are already connected (409)", async () => {
    mockSend.mockResolvedValue({ ok: false, code: "already_connected", message: "Connected." });
    const { onChange, container } = renderButton();
    await act(async () => fireEvent.click(button("Connect with Bob Babbage")));

    expect(onChange).toHaveBeenLastCalledWith({ status: "connected", requestId: null });
    expect(container.innerHTML).toBe("");
  });

  it("withdraws a sent request after an in-place confirmation", async () => {
    mockCancel.mockResolvedValue({ ok: true, data: undefined });
    const { onChange } = renderButton({ status: "pending_sent", requestId: requestID });

    fireEvent.click(button("Pending request to Bob Babbage"));
    const withdraw = button("Withdraw request to Bob Babbage");
    expect(withdraw.textContent).toBe("Withdraw request");
    expect(document.activeElement).toBe(withdraw);
    expect(mockCancel).not.toHaveBeenCalled();

    await act(async () => fireEvent.click(withdraw));
    expect(mockCancel).toHaveBeenCalledWith(requestID);
    expect(button("Connect with Bob Babbage")).toBeTruthy();
    expect(onChange).toHaveBeenLastCalledWith({ status: "none", requestId: null });
  });

  it("keeps the request when the confirmation is cancelled", () => {
    renderButton({ status: "pending_sent", requestId: requestID });
    fireEvent.click(button("Pending request to Bob Babbage"));
    fireEvent.click(button("Cancel"));

    noButton("Withdraw request to Bob Babbage");
    expect(document.activeElement).toBe(button("Pending request to Bob Babbage"));
    expect(mockCancel).not.toHaveBeenCalled();
  });

  it("goes back to Pending when withdrawing fails", async () => {
    const reply = deferred<Awaited<ReturnType<typeof cancelConnectionRequest>>>();
    mockCancel.mockReturnValue(reply.promise);
    renderButton({ status: "pending_sent", requestId: requestID });
    fireEvent.click(button("Pending request to Bob Babbage"));
    fireEvent.click(button("Withdraw request to Bob Babbage"));
    expect(button("Connect with Bob Babbage")).toBeTruthy();

    await act(async () => reply.resolve({ ok: false, code: "not_found", message: "Gone." }));
    expect(button("Pending request to Bob Babbage")).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toBe("Couldn't withdraw the request. Try again.");
  });

  it("accepts a received request, hides itself and refreshes the badge", async () => {
    mockAccept.mockResolvedValue({ ok: true, data: connection });
    const { onChange, container } = renderButton({ status: "pending_received", requestId: theirRequestID });
    expect(button("Accept request from Bob Babbage").textContent).toBe("Accept");
    expect(button("Ignore request from Bob Babbage").textContent).toBe("Ignore");

    await act(async () => fireEvent.click(button("Accept request from Bob Babbage")));
    expect(mockAccept).toHaveBeenCalledWith(theirRequestID);
    expect(onChange).toHaveBeenLastCalledWith({ status: "connected", requestId: null });
    expect(container.innerHTML).toBe("");
    expect(mockRefresh).toHaveBeenCalledWith(adaID);
  });

  it("ignores a received request and offers Connect", async () => {
    mockReject.mockResolvedValue({ ok: true, data: undefined });
    renderButton({ status: "pending_received", requestId: theirRequestID });
    await act(async () => fireEvent.click(button("Ignore request from Bob Babbage")));

    expect(mockReject).toHaveBeenCalledWith(theirRequestID);
    expect(button("Connect with Bob Babbage")).toBeTruthy();
    expect(mockRefresh).toHaveBeenCalledWith(adaID);
  });

  it("rolls an accept back when it fails", async () => {
    const reply = deferred<Awaited<ReturnType<typeof acceptConnectionRequest>>>();
    mockAccept.mockReturnValue(reply.promise);
    const { onChange } = renderButton({ status: "pending_received", requestId: theirRequestID });
    fireEvent.click(button("Accept request from Bob Babbage"));
    noButton("Accept request from Bob Babbage");

    await act(async () => reply.resolve({ ok: false, code: "network_error", message: "Offline." }));
    expect(button("Accept request from Bob Babbage")).toBeTruthy();
    expect(onChange).toHaveBeenLastCalledWith({ status: "pending_received", requestId: theirRequestID });
    expect(screen.getByRole("alert").textContent).toBe("Couldn't accept the request. Try again.");
    expect(mockRefresh).not.toHaveBeenCalled();
  });

  it.each(["connected", "self"] as const)("shows nothing when the status is %s", (status) => {
    const { container } = renderButton({ status });
    expect(container.innerHTML).toBe("");
  });

  it("follows new props, such as another row", () => {
    const { rerender } = renderButton();
    rerender(
      <ConnectButton userId={bobID} name="Bob Babbage" status="pending_sent" requestId={requestID} size="row" />,
    );
    expect(button("Pending request to Bob Babbage")).toBeTruthy();
  });

  it("draws Connect filled on the profile and outlined in a row", () => {
    const { rerender } = renderButton();
    expect(button("Connect with Bob Babbage").className).toContain("bg-rs-accent-strong");
    expect(button("Connect with Bob Babbage").className).toContain("h-10");
    rerender(<ConnectButton userId={bobID} name="Bob Babbage" status="none" requestId={null} size="row" />);
    expect(button("Connect with Bob Babbage").className).toContain("bg-rs-surface");
    expect(button("Connect with Bob Babbage").className).toContain("h-9");
  });
});
