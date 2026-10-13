"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { type FormEvent, useState } from "react";

import { buttonClass } from "@/components/ui/button";
import { Field, FormError } from "@/components/ui/field";

import { login, resendVerification } from "./api";
import { SENT_MESSAGE } from "./resend-form";
import { setSignedIn } from "./session";
import { type LoginErrors, type LoginValues, validateLogin } from "./validation";

// One message for an unknown address and a wrong password, so the page does not reveal
// which accounts exist (FR-2.1).
export const INVALID_CREDENTIALS_MESSAGE = "The email address or password is incorrect.";

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
    else if (res.code === "email_not_verified") setUnverified({ message: res.message, email });
    // The server's message says when the lock ends (FR-2.7).
    else if (res.code === "account_locked") setFormError(res.message);
    else setFormError(res.message);
  }

  return (
    <form noValidate onSubmit={onSubmit} className="space-y-4">
      {formError ? <FormError>{formError}</FormError> : null}
      {unverified ? <UnverifiedNotice {...unverified} /> : null}
      <Field
        id="email"
        name="email"
        label="Email"
        type="email"
        autoComplete="email"
        error={errors.email}
        value={values.email}
        onChange={(e) => setValues({ ...values, email: e.target.value })}
      />
      <div>
        <Field
          id="password"
          name="password"
          label="Password"
          type="password"
          autoComplete="current-password"
          error={errors.password}
          value={values.password}
          onChange={(e) => setValues({ ...values, password: e.target.value })}
        />
        <p className="mt-2 text-sm">
          <Link href="/forgot-password" className="font-semibold text-secondary hover:underline">
            Forgot password?
          </Link>
        </p>
      </div>
      <button type="submit" disabled={submitting} className={buttonClass({ variant: "primary", full: true })}>
        {submitting ? "Signing in…" : "Sign in"}
      </button>
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
    <div className="space-y-3 rounded-lg border border-danger/30 bg-danger/5 p-3">
      <p role="alert" className="text-sm text-danger">
        {message}
      </p>
      <button
        type="button"
        onClick={resend}
        disabled={sending}
        className={buttonClass({ variant: "outline", size: "md" })}
      >
        {sending ? "Sending…" : "Resend verification"}
      </button>
      <p role="status" className="text-sm text-text-muted">
        {status ?? null}
      </p>
    </div>
  );
}
