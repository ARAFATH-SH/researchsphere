"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { buttonClass } from "@/components/ui/button";

import { verifyEmail } from "./api";
import { AuthCard } from "./auth-card";
import { ResendForm } from "./resend-form";

export type Outcome = "checking" | "verified" | "used" | "expired" | "invalid" | "missing" | "error";

// tokenFromHash reads the token from a link such as /verify-email#token=abc. The token
// is in the fragment so that the browser never sends it to a server.
export function tokenFromHash(hash: string): string {
  return new URLSearchParams(hash.replace(/^#/, "")).get("token") ?? "";
}

// checkLink verifies the token in hash and says how it went.
export async function checkLink(hash: string): Promise<Outcome> {
  const token = tokenFromHash(hash);
  if (!token) return "missing";
  const res = await verifyEmail(token);
  if (res.ok) return "verified";
  switch (res.code) {
    case "token_used":
      return "used";
    case "token_expired":
      return "expired";
    case "token_invalid":
      return "invalid";
  }
  return "error";
}

const views: Record<Exclude<Outcome, "checking">, { title: string; text: string; resend: boolean; login: boolean }> = {
  verified: { title: "Email verified", text: "Your email address is verified. You can now sign in.", resend: false, login: true },
  used: {
    title: "Link already used",
    text: "This link has already been used. If you verified your address with it, you can sign in.",
    resend: false,
    login: true,
  },
  expired: {
    title: "Link expired",
    text: "This link has expired: links work for 24 hours. Ask for a new one below.",
    resend: true,
    login: false,
  },
  invalid: {
    title: "Link not valid",
    text: "This link is not valid. It may have been replaced by a newer one. Ask for a new link below.",
    resend: true,
    login: false,
  },
  missing: {
    title: "No verification link",
    text: "Open the link from your verification email. To get a new email, enter your address below.",
    resend: true,
    login: false,
  },
  error: {
    title: "Could not verify",
    text: "Something went wrong while checking the link. Reload the page to try again.",
    resend: false,
    login: false,
  },
};

// The check of each token, shared by every instance of the page. Strict Mode runs effects
// twice and the page can be mounted again; a second request would find the token used.
const checks = new Map<string, Promise<Outcome>>();

function checkOnce(hash: string): Promise<Outcome> {
  const token = tokenFromHash(hash);
  let check = checks.get(token);
  if (!check) {
    check = checkLink(hash);
    if (token) {
      checks.set(token, check);
      // An error spent nothing: a later attempt may ask again.
      void check.then((outcome) => {
        if (outcome === "error") checks.delete(token);
      });
    }
  }
  return check;
}

// VerifyEmail spends the token from the verification link once and shows the result
// (FR-1.3), with a way to get a new link when it cannot be used (FR-1.5).
export function VerifyEmail() {
  const [outcome, setOutcome] = useState<Outcome>("checking");

  useEffect(() => {
    let active = true;
    void checkOnce(window.location.hash).then((next) => {
      // The token is spent or refused: take it out of the address bar so it does not stay
      // in history. Keep history.state: the Next.js router keeps its own state there. After
      // an error it stays, so reloading the page tries again.
      if (next !== "error" && window.location.hash) {
        window.history.replaceState(window.history.state, "", window.location.pathname + window.location.search);
      }
      if (active) setOutcome(next);
    });
    return () => {
      active = false;
    };
  }, []);

  if (outcome === "checking") {
    return (
      <AuthCard title="Verifying your email">
        <p role="status">Checking your link…</p>
      </AuthCard>
    );
  }

  const view = views[outcome];
  return (
    <AuthCard title={view.title} signInLink={!view.login}>
      <p role="status">{view.text}</p>
      {view.login ? (
        <Link href="/login" className={buttonClass({ variant: "primary", full: true })}>
          Sign in
        </Link>
      ) : null}
      {view.resend ? <ResendForm /> : null}
    </AuthCard>
  );
}
