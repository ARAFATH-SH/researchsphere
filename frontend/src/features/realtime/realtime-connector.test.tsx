import { render } from "@testing-library/react";

import type { Session } from "@/features/auth/session";
import PublicLayout from "@/app/(public)/layout";

import { FakeWebSocket } from "./fake-websocket";
import { RealtimeConnector } from "./realtime-connector";
import { stopSocket } from "./socket";

const mockSession = jest.fn<Session, []>();
jest.mock("@/features/auth/session", () => ({ useSession: () => mockSession() }));
jest.mock("@/lib/api/client", () => ({ refreshSession: jest.fn(), reportSessionLost: jest.fn(), api: {} }));

const signedIn: Session = { status: "signedIn", user: { id: "u1", email: "a@example.org", fullName: "Ada", role: "researcher" } };

beforeEach(() => FakeWebSocket.install());
afterEach(stopSocket);

describe("RealtimeConnector", () => {
  it("connects while signed in, to the default dev address", () => {
    mockSession.mockReturnValue(signedIn);
    render(<RealtimeConnector />);
    expect(FakeWebSocket.instances).toHaveLength(1);
    expect(FakeWebSocket.latest().url).toBe("ws://localhost:8080/api/v1/ws");
  });

  it("uses NEXT_PUBLIC_WS_URL when it is set", () => {
    process.env.NEXT_PUBLIC_WS_URL = "wss://example.org/api/v1/ws";
    mockSession.mockReturnValue(signedIn);
    render(<RealtimeConnector />);
    delete process.env.NEXT_PUBLIC_WS_URL;
    expect(FakeWebSocket.latest().url).toBe("wss://example.org/api/v1/ws");
  });

  it.each<[string, Session]>([
    ["signed out", { status: "signedOut" }],
    ["still loading", { status: "loading" }],
  ])("does not connect while %s", (_, session) => {
    mockSession.mockReturnValue(session);
    render(<RealtimeConnector />);
    expect(FakeWebSocket.instances).toHaveLength(0);
  });

  it("closes the socket on sign-out", () => {
    mockSession.mockReturnValue(signedIn);
    const view = render(<RealtimeConnector />);
    const socket = FakeWebSocket.latest();
    mockSession.mockReturnValue({ status: "signedOut" });
    view.rerender(<RealtimeConnector />);
    expect(socket.clientClose).toBe(1000);
    expect(FakeWebSocket.instances).toHaveLength(1);
  });

  it("is on no public page, even for a signed-in visitor", () => {
    mockSession.mockReturnValue(signedIn);
    render(
      <PublicLayout params={Promise.resolve({})}>
        <p>Landing</p>
      </PublicLayout>,
    );
    expect(FakeWebSocket.instances).toHaveLength(0);
  });
});
