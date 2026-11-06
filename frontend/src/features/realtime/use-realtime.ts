import { useEffect, useRef } from "react";

import type { RealtimeEvent } from "./events";
import { onConnected, onRealtimeEvent } from "./socket";

function useLatest<T>(value: T) {
  const latest = useRef(value);
  useEffect(() => {
    latest.current = value;
  });
  return latest;
}

// useRealtimeEvent runs handler for every server event while the component is mounted.
export function useRealtimeEvent(handler: (event: RealtimeEvent) => void): void {
  const latest = useLatest(handler);
  useEffect(() => onRealtimeEvent((event) => latest.current(event)), [latest]);
}

// useOnConnected runs handler after every connect and reconnect while the component is mounted.
export function useOnConnected(handler: () => void): void {
  const latest = useLatest(handler);
  useEffect(() => onConnected(() => latest.current()), [latest]);
}
