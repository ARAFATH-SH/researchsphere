"use client";

import { useSyncExternalStore } from "react";

import type { Autosaver, SaveState } from "./autosave";

const savedState: SaveState = { status: "saved" };
const noSubscription = () => () => {};
const getSaved = () => savedState;

// useSaveState follows a saver's status for rendering; no saver reads as saved.
export function useSaveState(saver: Autosaver | null): SaveState {
  return useSyncExternalStore(saver?.subscribe ?? noSubscription, saver?.getState ?? getSaved, saver?.getState ?? getSaved);
}
