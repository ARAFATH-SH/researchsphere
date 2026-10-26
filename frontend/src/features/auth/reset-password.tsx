"use client";

import Link from "next/link";
import { type FormEvent, useState, useSyncExternalStore } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Field } from "@/components/ui/field";

import { confirmPasswordReset } from "./api";
import { AuthCard } from "./auth-card";
import { AuthAlert, noticeClass } from "./notice";
import { type NewPasswordErrors, type NewPasswordValues, validateNewPassword } from "./validation";
import { tokenFromHash } from "./verify-email";

type LinkProblem = "missing" | "invalid" | "used" | "expired";

const problems: Record<LinkProblem, { title: string; text: string }> = {
  missing: { title: "No reset link", text: "Open the link from your password reset email, or ask for a new one." },
  invalid: {
    title: "Link not valid",
    text: "This reset link is not valid. It may have been replaced by a newer one. Ask for a new link.",
  },
  used: { title: "Link already used", text: "This reset link has already been used. Ask for a new one if you still need it." },
  expired: { title: "Link expired", text: "This reset link has expired: links work for 1 hour. Ask for a new one." },
};

function problemForCode(code: string): LinkProblem | undefined {
  switch (code) {
    case "token_invalid":
      return "invalid";
    case "token_used":
      return "used";
    case "token_expired":
      return "expired";
  }
  return undefined;
}

// The token in the reset link (/reset-password#token=...) is read from the address bar on
// every render, so a remount or a reload finds it again. It stays there until the link is
// spent or refused; then forgetLink takes it out so it does not stay in history.
function subscribeHash(onChange: () => void): () => void {
  window.addEventListener("hashchange", onChange);
  return () => window.removeEventListener("hashchange", onChange);
}

const linkToken = () => tokenFromHash(window.location.hash);
const noToken = () => undefined;

function forgetLink(): void {
  // Keep history.state: the Next.js router keeps its own state there.
  if (window.location.hash) window.history.replaceState(window.history.state, "", window.location.pathname + window.location.search);
}

// ResetPassword sets a new password with the token from a reset link (FR-2.3). The
// server then ends every session of the account (FR-2.5). It calls the API only when the
// form is submitted.
export function ResetPassword() {
  // undefined on the server and during hydration; "" when the link has no token.
  const token = useSyncExternalStore(subscribeHash, linkToken, noToken);
  const [values, setValues] = useState<NewPasswordValues>({ password: "", confirmPassword: "" });
  const [errors, setErrors] = useState<NewPasswordErrors>({});
  const [formError, setFormError] = useState<string>();
  const [submitting, setSubmitting] = useState(false);
  const [problem, setProblem] = useState<LinkProblem>();
  const [done, setDone] = useState(false);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(undefined);
    const found = validateNewPassword(values);
    setErrors(found);
    if (Object.keys(found).length > 0 || !token) return;

    setSubmitting(true);
    const res = await confirmPasswordReset(token, values.password);
    setSubmitting(false);
    if (res.ok) {
      forgetLink();
      setDone(true);
      return;
    }
    const linkProblem = problemForCode(res.code);
    if (linkProblem) {
      forgetLink();
      setProblem(linkProblem);
    } else if (res.code === "password_too_short" || res.code === "password_too_long") {
      setErrors({ password: res.message });
    } else {
      setFormError(res.message);
    }
  }

  if (done) {
    return (
      <AuthCard title="Password changed">
        <p role="status" className={noticeClass("success")}>
          Your password has been changed and you have been signed out on every device. Sign in with your new password.
        </p>
        <Link href="/login" className={rsButtonClass({ full: true })}>
          Sign in
        </Link>
      </AuthCard>
    );
  }

  const shown = problem ?? (token === "" ? "missing" : undefined);
  if (shown) {
    const view = problems[shown];
    return (
      <AuthCard title={view.title} signInLink>
        <p role="status" className={noticeClass("error")}>
          {view.text}
        </p>
        <Link href="/forgot-password" className={rsButtonClass({ full: true })}>
          Ask for a new link
        </Link>
      </AuthCard>
    );
  }

  return (
    <AuthCard title="Choose a new password" signInLink>
      <form noValidate onSubmit={onSubmit} className="flex flex-col gap-3.5">
        {formError ? <AuthAlert>{formError}</AuthAlert> : null}
        <Field
          id="password"
          name="password"
          label="New password"
          type="password"
          autoComplete="new-password"
          look="rs"
          hint="At least 8 characters."
          error={errors.password}
          value={values.password}
          onChange={(e) => setValues({ ...values, password: e.target.value })}
        />
        <Field
          id="confirmPassword"
          name="confirmPassword"
          label="Confirm new password"
          type="password"
          autoComplete="new-password"
          look="rs"
          error={errors.confirmPassword}
          value={values.confirmPassword}
          onChange={(e) => setValues({ ...values, confirmPassword: e.target.value })}
        />
        {/* The design's helper line above the button. */}
        <p className="text-rs-caption text-rs-text-muted">Saving signs you out on every device.</p>
        <button type="submit" disabled={submitting || token === undefined} className={rsButtonClass({ full: true })}>
          {submitting ? "Saving…" : "Set new password"}
        </button>
      </form>
    </AuthCard>
  );
}
