"use client";

import { type FormEvent, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Field } from "@/components/ui/field";

import { requestPasswordReset } from "./api";
import { StatusNotice } from "./notice";
import { messages, validateEmail } from "./validation";

// The same text whether or not the address belongs to an account (FR-2.4).
export const RESET_SENT_MESSAGE = "If an account exists for that address, we sent a link to reset the password. It works for 1 hour.";

// ForgotPasswordForm asks for a password reset link (FR-2.3).
export function ForgotPasswordForm() {
  const [email, setEmail] = useState("");
  const [error, setError] = useState<string>();
  const [sent, setSent] = useState(false);
  const [sending, setSending] = useState(false);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSent(false);
    const invalid = email.trim() === "" ? messages.emailRequired : validateEmail(email);
    setError(invalid);
    if (invalid) return;

    setSending(true);
    const res = await requestPasswordReset(email.trim());
    setSending(false);
    if (res.ok) setSent(true);
    else setError(res.message);
  }

  return (
    <form noValidate onSubmit={onSubmit} className="flex flex-col gap-3.5">
      <Field
        id="email"
        name="email"
        label="Email"
        type="email"
        autoComplete="email"
        look="rs"
        value={email}
        error={error}
        onChange={(e) => setEmail(e.target.value)}
      />
      <button type="submit" disabled={sending} className={rsButtonClass({ full: true })}>
        {sending ? "Sending…" : "Send reset link"}
      </button>
      <StatusNotice>{sent ? RESET_SENT_MESSAGE : null}</StatusNotice>
    </form>
  );
}
