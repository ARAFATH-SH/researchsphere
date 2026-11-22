import { HocuspocusProvider, HocuspocusProviderWebsocket, WebSocketStatus } from "@hocuspocus/provider";
import type { Awareness } from "y-protocols/awareness";
import type * as Y from "yjs";

import { syncUrl } from "./types";

// A Link is one file's connection to the sync server (docs/sync.md). The session talks to this
// interface only, so tests replace it with an in-memory server (fake-hub.ts) and no other
// module imports @hocuspocus/provider.

export interface LinkEvents {
  /** The first sync of this connection is done. */
  synced(): void;
  /** A stateless message: session, presence or stored. */
  stateless(payload: string): void;
  /** The server refused the connection (authenticationFailed); reason as in docs/sync.md. */
  refused(reason: string): void;
  /** The server ended this document's connection; reason as in docs/sync.md. */
  closed(reason: string): void;
  /** The WebSocket was lost; the link reconnects by itself and asks for a new token. */
  disconnected(): void;
  /** Changes the server has not acknowledged yet. */
  unsynced(count: number): void;
}

export interface Link {
  /** Changes the server has not acknowledged yet. */
  unsynced(): number;
  /** Authenticates again on the open socket (a new token), after a refusal or a close. */
  reconnect(): void;
  /** Leaves the document on the server and stops; the awareness is destroyed with it. */
  destroy(): void;
}

export type LinkOptions = {
  fileId: string;
  doc: Y.Doc;
  awareness: Awareness;
  /** Called on every connect: a ticket works once (docs/sync.md). */
  token: () => Promise<string>;
  events: LinkEvents;
};

export type LinkFactory = (options: LinkOptions) => Link;

// One WebSocket per tab carries every open file (docs/sync.md); it closes with the last file.
let shared: { socket: HocuspocusProviderWebsocket; users: number } | null = null;

function socket(): HocuspocusProviderWebsocket {
  if (!shared) {
    shared = {
      socket: new HocuspocusProviderWebsocket({
        url: syncUrl(),
        delay: 1000,
        factor: 2,
        maxDelay: 30_000,
        minDelay: 1000,
        jitter: true,
        maxAttempts: 0,
      }),
      users: 0,
    };
  }
  shared.users++;
  return shared.socket;
}

function release(): void {
  if (!shared) return;
  shared.users--;
  if (shared.users === 0) {
    shared.socket.destroy();
    shared = null;
  }
}

export const hocuspocusLink: LinkFactory = ({ fileId, doc, awareness, token, events }) => {
  const ws = socket();
  let alive = true;
  const provider = new HocuspocusProvider({
    websocketProvider: ws,
    name: fileId,
    document: doc,
    awareness,
    token,
    onSynced: () => alive && events.synced(),
    onStateless: ({ payload }) => alive && events.stateless(payload),
    onAuthenticationFailed: ({ reason }) => alive && events.refused(reason),
    // A close message for this document arrives on an open socket; a lost socket is closed
    // already when this runs (the socket's own listener goes first).
    onClose: ({ event }) => {
      if (alive && ws.status === WebSocketStatus.Connected) events.closed(event.reason);
    },
    onDisconnect: () => alive && events.disconnected(),
    onUnsyncedChanges: ({ number }) => alive && events.unsynced(number),
  });
  provider.attach();
  return {
    unsynced: () => provider.unsyncedChanges,
    reconnect() {
      provider.detach();
      provider.attach();
    },
    destroy() {
      if (!alive) return;
      alive = false;
      provider.destroy();
      release();
    },
  };
};
