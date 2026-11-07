// FakeWebSocket stands in for the browser's WebSocket in tests. Install it with
// FakeWebSocket.install(); FakeWebSocket.latest() is the newest socket, and accept, receive and
// serverClose play the server.
export class FakeWebSocket {
  static instances: FakeWebSocket[] = [];

  static install(): void {
    FakeWebSocket.instances = [];
    globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
  }

  static latest(): FakeWebSocket {
    const socket = FakeWebSocket.instances.at(-1);
    if (!socket) throw new Error("no socket was opened");
    return socket;
  }

  readyState = 0;
  sent: { type: string; data: unknown }[] = [];
  clientClose: number | null = null;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: ((event: { code: number }) => void) | null = null;

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  send(frame: string): void {
    this.sent.push(JSON.parse(frame));
  }

  close(code = 1005): void {
    this.clientClose = code;
    this.readyState = 3;
    this.onclose?.({ code });
  }

  accept(): void {
    this.readyState = 1;
    this.onopen?.();
  }

  receive(type: string, data: unknown): void {
    this.onmessage?.({ data: JSON.stringify({ type, data }) });
  }

  serverClose(code: number): void {
    this.readyState = 3;
    this.onclose?.({ code });
  }
}
