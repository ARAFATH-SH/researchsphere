"use client";

import { useCallback, useEffect, useId, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { FormError } from "@/components/ui/field";

import { getMySettings, setUnreadMessageDigest } from "./api";

type State = { status: "loading" } | { status: "error" } | { status: "ok"; on: boolean };

// DigestToggle is the "Email me about unread messages" switch of the Settings card "Messaging &
// email" (design/screens/Settings.html, FR-16.11). It changes at once and goes back, with an
// error, when the server refuses.
export function DigestToggle() {
  const [state, setState] = useState<State>({ status: "loading" });
  const [saving, setSaving] = useState(false);
  const [saveFailed, setSaveFailed] = useState(false);
  const labelId = useId();
  const hintId = useId();

  const load = useCallback(async () => {
    setState({ status: "loading" });
    const res = await getMySettings();
    setState(res.ok ? { status: "ok", on: res.data.unreadMessageDigest } : { status: "error" });
  }, []);

  useEffect(() => {
    let active = true;
    void getMySettings().then((res) => {
      if (active) setState(res.ok ? { status: "ok", on: res.data.unreadMessageDigest } : { status: "error" });
    });
    return () => {
      active = false;
    };
  }, []);

  async function toggle() {
    if (state.status !== "ok" || saving) return;
    const next = !state.on;
    setState({ status: "ok", on: next });
    setSaving(true);
    setSaveFailed(false);
    const res = await setUnreadMessageDigest(next);
    setSaving(false);
    if (res.ok) {
      setState({ status: "ok", on: res.data.unreadMessageDigest });
    } else {
      setState({ status: "ok", on: !next });
      setSaveFailed(true);
    }
  }

  const on = state.status === "ok" && state.on;
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span>
          <span id={labelId} className="block text-rs-body font-bold text-rs-text">
            Email me about unread messages
          </span>
          <span id={hintId} className="text-rs-caption text-rs-text-muted">
            A daily digest of messages left unread for 24 hours.
          </span>
        </span>
        {state.status !== "error" && (
          <button
            type="button"
            role="switch"
            aria-checked={on}
            aria-labelledby={labelId}
            aria-describedby={hintId}
            disabled={state.status === "loading" || saving}
            onClick={toggle}
            className={`relative h-7 w-12 flex-none rounded-full transition-colors focus-visible:shadow-focus focus-visible:outline-none disabled:cursor-wait ${
              on ? "bg-rs-accent-strong" : "bg-rs-control"
            }`}
          >
            <span
              aria-hidden="true"
              className={`absolute top-[3px] size-[22px] rounded-full bg-rs-surface transition-[left] ${on ? "left-[23px]" : "left-[3px]"}`}
            />
          </button>
        )}
      </div>
      {state.status === "error" && (
        <div className="flex flex-col items-start gap-2">
          <FormError look="rs">Couldn&apos;t load your email setting.</FormError>
          <button type="button" onClick={load} className={rsButtonClass({ variant: "secondary", size: "xs" })}>
            Try again
          </button>
        </div>
      )}
      {saveFailed && <FormError look="rs">Couldn&apos;t save the setting. Try again.</FormError>}
    </div>
  );
}
