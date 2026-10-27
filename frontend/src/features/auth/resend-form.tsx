"use client";

import { type FormEvent, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Field } from "@/components/ui/field";

import { resendVerification } from "./api";
import { StatusNotice } from "./notice";
import { usePendingEmail } from "./pending-email";
import { validateEmail } from "./validation";

// The same text whatever the server knows about the address (FR-1.5).
export const SENT_MESSAGE =
  "If that address belongs to an account that still needs verifying, we have sent a new link. It works for 24 hours.";

// ResendForm asks for a new verification link (FR-1.5). It starts with the address the
// last link went to in this tab, when there is one.
export function ResendForm() {
  const stored = usePendingEmail();
  const [typed, setTyped] = useState<string>();
  const email = typed ?? stored;
  const [error, setError] = useState<string>();
  const [sent, setSent] = useState(false);
  const [sending, setSending] = useState(false);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSent(false);
    const invalid = validateEmail(email);
    setError(invalid);
    if (invalid) return;

    setSending(true);
    const res = await resendVerification(email.trim());
    setSending(false);
    if (res.ok) setSent(true);
    else setError(res.message);
  }

  return (
    <form noValidate onSubmit={onSubmit} className="flex flex-col gap-3.5">
      <Field
        id="resend-email"
        label="Email"
        type="email"
        autoComplete="email"
        look="rs"
        value={email}
        error={error}
        onChange={(e) => setTyped(e.target.value)}
      />
      <button type="submit" disabled={sending} className={rsButtonClass({ variant: "outline", size: "md", full: true })}>
        {sending ? "Sending…" : "Send a new link"}
      </button>
      <StatusNotice>{sent ? SENT_MESSAGE : null}</StatusNotice>
    </form>
  );
}
