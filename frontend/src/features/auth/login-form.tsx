"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { type FormEvent, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Field } from "@/components/ui/field";

import { login, resendVerification } from "./api";
import { authLinkClass } from "./auth-card";
import { AuthAlert, noticeClass, noticeLinkClass } from "./notice";
import { SENT_MESSAGE } from "./resend-form";
import { setSignedIn } from "./session";
import { type LoginErrors, type LoginValues, validateLogin } from "./validation";

// One message for an unknown address and a wrong password, so the page does not reveal
// which accounts exist (FR-2.1).
export const INVALID_CREDENTIALS_MESSAGE = "The email address or password is incorrect.";
const SUSPENDED_MESSAGE = "This account is suspended. Contact support.";

// LoginForm signs a user in (FR-2.1) and goes to /home. An address that is not verified
// yet is refused, with a button to send a new verification link (FR-1.4, FR-1.5). A
// locked account is refused with the time to wait (FR-2.7).
export function LoginForm() {
  const router = useRouter();
  const [values, setValues] = useState<LoginValues>({ email: "", password: "" });
  const [errors, setErrors] = useState<LoginErrors>({});
  const [formError, setFormError] = useState<string>();
  const [unverified, setUnverified] = useState<{ message: string; email: string }>();
  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(undefined);
    setUnverified(undefined);
    const found = validateLogin(values);
    setErrors(found);
    if (Object.keys(found).length > 0) return;

    setSubmitting(true);
    const email = values.email.trim();
    const res = await login({ email, password: values.password });
    if (res.ok) {
      setSignedIn(res.data);
      router.replace("/home");
      return;
    }
    setSubmitting(false);
    if (res.code === "invalid_credentials") setFormError(INVALID_CREDENTIALS_MESSAGE);
    else if (res.code === "account_suspended") setFormError(SUSPENDED_MESSAGE); // FR-3.4
    else if (res.code === "email_not_verified") setUnverified({ message: res.message, email });
    // The server's message says when the lock ends (FR-2.7).
    else if (res.code === "account_locked") setFormError(res.message);
    else setFormError(res.message);
  }

  return (
    <form noValidate onSubmit={onSubmit} className="flex flex-col gap-3.5">
      {formError ? <AuthAlert>{formError}</AuthAlert> : null}
      <Field
        id="email"
        name="email"
        label="Email"
        type="email"
        autoComplete="email"
        look="rs"
        error={errors.email}
        value={values.email}
        onChange={(e) => setValues({ ...values, email: e.target.value })}
      />
      <Field
        id="password"
        name="password"
        label="Password"
        type="password"
        autoComplete="current-password"
        look="rs"
        error={errors.password}
        value={values.password}
        onChange={(e) => setValues({ ...values, password: e.target.value })}
      />
      <Link href="/forgot-password" className={`self-start ${authLinkClass}`}>
        Forgot password?
      </Link>
      <button type="submit" disabled={submitting} className={rsButtonClass({ full: true })}>
        {submitting ? "Signing in…" : "Sign in"}
      </button>
      {/* Under the button, as in the design. */}
      {unverified ? <UnverifiedNotice {...unverified} /> : null}
    </form>
  );
}

// UnverifiedNotice shows the server's message and sends a new link to the address
// the user tried to sign in with.
function UnverifiedNotice({ message, email }: { message: string; email: string }) {
  const [sending, setSending] = useState(false);
  const [status, setStatus] = useState<string>();

  async function resend() {
    setSending(true);
    const res = await resendVerification(email);
    setSending(false);
    setStatus(res.ok ? SENT_MESSAGE : res.message);
  }

  return (
    <div className={`flex flex-col gap-2 ${noticeClass("warning")}`}>
      <p>
        <span role="alert">{message}</span>{" "}
        <button type="button" onClick={resend} disabled={sending} className={noticeLinkClass}>
          {sending ? "Sending…" : "Resend verification"}
        </button>
      </p>
      <p role="status" className={status ? undefined : "sr-only"}>
        {status ?? null}
      </p>
    </div>
  );
}
