"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { AuthCard, authLinkClass } from "@/features/auth/auth-card";
import { noticeClass } from "@/features/auth/notice";

import { unsubscribeDigest } from "./api";

export type UnsubscribeOutcome = "working" | "done" | "invalid" | "missing" | "error";

// SETTINGS_HREF is where the digest is turned back on; it needs a sign-in.
export const SETTINGS_HREF = "/settings/account#messaging-email";

// turnOff spends nothing: a token only ever turns the digest off, so trying again is safe.
export async function turnOff(token: string): Promise<UnsubscribeOutcome> {
  if (!token) return "missing";
  const res = await unsubscribeDigest(token);
  if (res.ok) return "done";
  return res.code === "invalid_token" ? "invalid" : "error";
}

// Unsubscribe turns the unread message digest off with the token from its email (FR-16.11). It
// needs no sign-in. The request is sent from this script after the page loads, never by the
// GET of the page itself, so a mail scanner that only fetches the link changes nothing.
export function Unsubscribe() {
  const [outcome, setOutcome] = useState<UnsubscribeOutcome>("working");

  const run = useCallback(async () => {
    setOutcome("working");
    const token = new URLSearchParams(window.location.search).get("token") ?? "";
    setOutcome(await turnOff(token));
  }, []);

  useEffect(() => {
    let active = true;
    const token = new URLSearchParams(window.location.search).get("token") ?? "";
    void turnOff(token).then((next) => {
      if (active) setOutcome(next);
    });
    return () => {
      active = false;
    };
  }, []);

  switch (outcome) {
    case "working":
      return (
        <AuthCard title="Unread message emails">
          <p role="status">Turning off unread message emails…</p>
        </AuthCard>
      );
    case "done":
      return (
        <AuthCard title="Unread message emails are off">
          <p role="status" className={noticeClass("success")}>
            You won&apos;t get unread message emails any more.
          </p>
          <p className="text-rs-body text-rs-text-muted">
            Changed your mind?{" "}
            <Link href={SETTINGS_HREF} className={authLinkClass}>
              Turn it back on in Settings
            </Link>
          </p>
        </AuthCard>
      );
    case "invalid":
    case "missing":
      return (
        <AuthCard title="Link not valid">
          <p role="status" className={noticeClass("error")}>
            {outcome === "missing"
              ? "This page needs the link from an unread message email."
              : "This unsubscribe link is not valid. Copy the whole link from the email, or change the setting in Settings."}
          </p>
          <Link href={SETTINGS_HREF} className={rsButtonClass({ full: true })}>
            Open Settings
          </Link>
        </AuthCard>
      );
    case "error":
      return (
        <AuthCard title="Unread message emails">
          <p role="alert" className={noticeClass("error")}>
            Something went wrong. Your emails were not turned off yet.
          </p>
          <button type="button" onClick={run} className={rsButtonClass({ full: true })}>
            Try again
          </button>
        </AuthCard>
      );
  }
}
