import { useSyncExternalStore } from "react";

import type { RefreshOutcome } from "@/lib/api/auth-fetch";

import { backoffDelay, closeAction } from "./close-policy";
import { parseEvent, type RealtimeEvent } from "./events";

// The one socket of this browser tab (docs/realtime.md). It lives outside React, so moving
// between pages keeps it open; RealtimeConnector starts it while someone is signed in.
//
// idle: not started. connecting: a socket is opening. open: connected. waiting: closed, and
// refreshing or waiting to try again. replaced: closed with 4409 and staying away until the tab
// is shown again.
export type SocketStatus = "idle" | "connecting" | "open" | "waiting" | "replaced";

export type SocketOptions = {
  url: string;
  refresh: () => Promise<RefreshOutcome>;
  onSessionLost: () => void;
};

// stableAfter is how long a connection must last before the backoff starts again from 1 s.
export const stableAfter = 30_000;

const wsOpen = 1;

let options: SocketOptions | null = null;
let socket: WebSocket | null = null;
let status: SocketStatus = "idle";
let attempts = 0;
let refreshedForHandshake = false;
let retryTimer: ReturnType<typeof setTimeout> | null = null;
let stableTimer: ReturnType<typeof setTimeout> | null = null;

const eventListeners = new Set<(event: RealtimeEvent) => void>();
const connectedListeners = new Set<() => void>();
const statusListeners = new Set<() => void>();

function setStatus(next: SocketStatus): void {
  if (next === status) return;
  status = next;
  statusListeners.forEach((listener) => listener());
}

function clearTimer(timer: ReturnType<typeof setTimeout> | null): null {
  if (timer !== null) clearTimeout(timer);
  return null;
}

function open(current: SocketOptions): void {
  const ws = new WebSocket(current.url);
  socket = ws;
  let opened = false;
  setStatus("connecting");

  ws.onopen = () => {
    if (ws !== socket) return;
    opened = true;
    refreshedForHandshake = false;
    setStatus("open");
    stableTimer = setTimeout(() => {
      attempts = 0;
    }, stableAfter);
    connectedListeners.forEach((listener) => listener());
  };
  ws.onmessage = ({ data }) => {
    if (ws !== socket) return;
    const event = parseEvent(data);
    if (event) eventListeners.forEach((listener) => listener(event));
  };
  ws.onclose = ({ code }) => {
    if (ws !== socket) return;
    socket = null;
    stableTimer = clearTimer(stableTimer);
    closed(current, code, opened);
  };
}

function closed(current: SocketOptions, code: number, opened: boolean): void {
  if (code === 1009) console.warn("realtime: the server refused a frame as too large");
  const action = closeAction(code, opened);
  if (action === "replaced") {
    setStatus("replaced");
    return;
  }
  setStatus("waiting");
  if (action === "refresh" || (action === "handshake" && !refreshedForHandshake)) {
    // Once per run of failed handshakes: the REST fallback refreshes on its own after that, and
    // a long outage should not rotate the token at every attempt.
    if (action === "handshake") refreshedForHandshake = true;
    void refreshThenRetry(current);
    return;
  }
  retry(current);
}

async function refreshThenRetry(current: SocketOptions): Promise<void> {
  const outcome = await current.refresh();
  if (current !== options) return; // stopped meanwhile
  if (outcome === "refused") {
    stopSocket();
    current.onSessionLost();
    return;
  }
  retry(current);
}

function retry(current: SocketOptions): void {
  retryTimer = setTimeout(() => open(current), backoffDelay(attempts++));
}

function onVisibilityChange(): void {
  if (status === "replaced" && options && document.visibilityState === "visible") {
    attempts = 0;
    open(options);
  }
}

// startSocket connects, replacing any socket already running.
export function startSocket(next: SocketOptions): void {
  stopSocket();
  options = next;
  document.addEventListener("visibilitychange", onVisibilityChange);
  open(next);
}

// stopSocket closes the socket and cancels everything pending; nothing reconnects after it.
export function stopSocket(): void {
  if (!options) return;
  options = null;
  document.removeEventListener("visibilitychange", onVisibilityChange);
  retryTimer = clearTimer(retryTimer);
  stableTimer = clearTimer(stableTimer);
  attempts = 0;
  refreshedForHandshake = false;
  const ws = socket;
  socket = null;
  ws?.close(1000);
  setStatus("idle");
}

// sendEvent sends one frame while connected, and reports whether it went. Nothing is queued:
// the socket carries only typing, which is worthless later.
export function sendEvent(type: "typing.start" | "typing.stop", conversationId: string): boolean {
  if (socket?.readyState !== wsOpen) return false;
  socket.send(JSON.stringify({ type, data: { conversationId } }));
  return true;
}

function listen<T>(listeners: Set<T>, listener: T): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

// onRealtimeEvent hears every server event; the returned function stops listening.
export function onRealtimeEvent(listener: (event: RealtimeEvent) => void): () => void {
  return listen(eventListeners, listener);
}

// onConnected runs after every connect and reconnect, when whatever was missed must be fetched:
// events are not stored, and the channel is live before the socket opens, so nothing falls
// between the two.
export function onConnected(listener: () => void): () => void {
  return listen(connectedListeners, listener);
}

export function onStatusChange(listener: () => void): () => void {
  return listen(statusListeners, listener);
}

export function socketStatus(): SocketStatus {
  return status;
}

export function useSocketStatus(): SocketStatus {
  return useSyncExternalStore(onStatusChange, socketStatus, () => "idle");
}
