import type { Awareness } from "y-protocols/awareness";
import type * as Y from "yjs";

import type { ProjectRole } from "../../api";
import type { Autosaver, SaveState } from "../autosave";

// Real-time editing on the client (FR-12.4 to FR-12.9, NFR-2; docs/sync.md). Types only: the
// workspace imports them without loading Yjs, which comes with collab/session.ts on demand.

/** The Y.Text that holds a file's text on the sync server (sync/src/server.ts). */
export const textName = "content";

/** A member in a file, from the server's presence message. */
export type PresenceUser = { id: string; name: string; colour: string; readOnly: "capacity" | null };

/** Who edits which file of the project: fileId -> members, earliest first. */
export type PresenceFiles = Record<string, PresenceUser[]>;

/** Why the file cannot be edited by this member now (docs/sync.md, session message). */
export type ReadOnlyReason = "viewer" | "capacity" | null;

/**
 * How the file is connected:
 * connecting: no live document yet; live: on the sync server; offline: the connection is lost
 * and edits stay in the document; fallback: the sync server cannot be reached, REST saves
 * (21.2) instead; ended: this member can no longer have the file open.
 */
export type CollabMode = "connecting" | "live" | "offline" | "fallback" | "ended";

/** Why a file was closed for this member. */
export type EndReason = "access_ended" | "file_gone" | "session_lost";

/** The text lives in a Yjs document on the sync server. */
export type YjsBinding = {
  kind: "yjs";
  doc: Y.Doc;
  text: Y.Text;
  awareness: Awareness;
  undoManager: Y.UndoManager;
};

/** The text lives in an Autosaver that saves over REST (21.2). */
export type RestBinding = { kind: "rest"; saver: Autosaver };

export type Binding = YjsBinding | RestBinding;

/** Two texts of the file that cannot be merged: the member chooses (never silently). */
export type Conflict = { mine: string; theirs: string };

export type CollabState = {
  mode: CollabMode;
  /** What the editor shows; null until the first sync or REST read. */
  binding: Binding | null;
  /** Changes the sync server has not acknowledged yet. */
  unsynced: number;
  /** The server stored everything this client has seen (a "stored" message). */
  stored: boolean;
  role: ProjectRole | null;
  readOnly: ReadOnlyReason;
  maxEditors: number | null;
  colour: string | null;
  presence: PresenceFiles | null;
  ended: EndReason | null;
  /** The last edit would take the file over 1 MiB and was undone. */
  tooLarge: boolean;
  conflict: Conflict | null;
  /** Moving between REST and the sync server, or to a new history: no typing meanwhile. */
  switching: boolean;
  /** Neither the sync server nor the API answers yet: the file opens when one does. */
  waiting: boolean;
  /** Why live editing was refused (a sync server reason, `ticket_<code>`, `sync_unreachable`); development only. */
  fallbackReason: string | null;
};

export const initialState: CollabState = {
  mode: "connecting",
  binding: null,
  unsynced: 0,
  stored: false,
  role: null,
  readOnly: null,
  maxEditors: null,
  colour: null,
  presence: null,
  ended: null,
  tooLarge: false,
  conflict: null,
  switching: false,
  waiting: false,
  fallbackReason: null,
};

/** Whether this member may type in the file now. */
export function canType(state: CollabState, fallbackRole: ProjectRole): boolean {
  const role = state.role ?? fallbackRole;
  if (role === "viewer" || state.readOnly !== null || state.switching || state.ended || state.conflict) return false;
  if (state.binding === null) return false;
  return state.mode === "live" || state.mode === "offline" || state.mode === "fallback";
}

/** The status the workspace bar shows. saveState is the Autosaver's while saving over REST. */
export function collabStatus(state: CollabState, saveState: SaveState | null): { text: string; trouble: boolean; saved: boolean } {
  switch (state.mode) {
    case "connecting":
      return { text: "Connecting…", trouble: false, saved: false };
    case "offline":
      return { text: "Offline, changes will sync", trouble: true, saved: false };
    case "ended":
      return { text: "Closed", trouble: true, saved: false };
    case "fallback":
      return { text: fallbackNotice, trouble: true, saved: saveState?.status === "saved" };
    case "live":
      if (state.switching) return { text: "Connecting…", trouble: false, saved: false };
      if (state.unsynced > 0) return { text: "Saving…", trouble: false, saved: false };
      if (state.stored) return { text: "Saved", trouble: false, saved: true };
      return { text: "Live", trouble: false, saved: false };
  }
}

/** The FR-12.8 reason, built from the server's limit (docs/decisions.md, 2026-11-23). */
export function capacityText(maxEditors: number | null): string {
  const n = maxEditors ?? 4;
  return `This file already has ${n} people editing. You can read it and will be able to edit when someone leaves.`;
}

/** The notice while the file saves over REST (docs/sync.md, REST saves while a file is open). */
export const fallbackNotice = "Live editing unavailable, saving normally";

const reasonTexts: Record<string, string> = {
  origin_not_allowed: "the sync server refused this page’s origin; add it to SYNC_ALLOWED_ORIGINS",
  unavailable: "the sync server could not reach the API or Redis",
  sync_unreachable: "the sync server did not answer (is make sync running?)",
  too_many_connections: "too many files open on the sync server",
  shutting_down: "the sync server is stopping",
};

/** The developer's explanation of a refusal; shown only in development. */
export function fallbackReasonText(reason: string | null): string | null {
  if (!reason) return null;
  if (reasonTexts[reason]) return reasonTexts[reason];
  if (reason.startsWith("ticket_")) return `no ticket from the API (${reason.slice(7)})`;
  return reason.replace(/[^\w .:()-]/g, "").slice(0, 100);
}

/** A colour the server sent, accepted only as #rrggbb, so it is safe in a style. */
export function safeColour(colour: unknown): string | null {
  return typeof colour === "string" && /^#[0-9a-f]{6}$/i.test(colour) ? colour : null;
}

/** The sync server's WebSocket address (docs/sync.md, Connecting). */
export function syncUrl(): string {
  return process.env.NEXT_PUBLIC_SYNC_URL || "ws://localhost:8091";
}
