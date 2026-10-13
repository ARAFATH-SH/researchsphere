"use client";

import { type FormEvent, useState } from "react";

import { buttonClass } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Field, FormError } from "@/components/ui/field";

import { changePassword } from "./api";
import {
  type PasswordChangeErrors,
  type PasswordChangeField,
  type PasswordChangeValues,
  validatePasswordChange,
} from "./validation";

const empty: PasswordChangeValues = {
  currentPassword: "",
  newPassword: "",
  confirmPassword: "",
};

// fieldForChangeCode says which field a server error code of POST /auth/password belongs
// to (docs/openapi.yaml). Other codes, such as account_locked, go in the banner.
function fieldForChangeCode(code: string): PasswordChangeField | undefined {
  switch (code) {
    case "wrong_current_password":
      return "currentPassword";
    case "password_too_short":
    case "password_too_long":
    case "same_password":
      return "newPassword";
  }
  return undefined;
}

// ChangePasswordForm lets a signed-in user set a new password after entering the current
// one (FR-2.6). The server ends every other session of the account; this one stays signed
// in (FR-2.5).
export function ChangePasswordForm() {
  const [values, setValues] = useState<PasswordChangeValues>(empty);
  const [errors, setErrors] = useState<PasswordChangeErrors>({});
  const [formError, setFormError] = useState<string>();
  const [submitting, setSubmitting] = useState(false);
  const [done, setDone] = useState(false);

  function set(field: PasswordChangeField, value: string) {
    setValues({ ...values, [field]: value });
    setDone(false);
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(undefined);
    setDone(false);
    const found = validatePasswordChange(values);
    setErrors(found);
    if (Object.keys(found).length > 0) return;

    setSubmitting(true);
    const res = await changePassword({
      currentPassword: values.currentPassword,
      newPassword: values.newPassword,
    });
    setSubmitting(false);
    if (res.ok) {
      setValues(empty);
      setDone(true);
      return;
    }
    const field = fieldForChangeCode(res.code);
    if (field) setErrors({ [field]: res.message });
    else setFormError(res.message);
  }

  return (
    <section aria-labelledby="change-password-title">
      <Card className="grid gap-6 p-6 md:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
        <div>
          <h2 id="change-password-title" className="text-lg font-semibold">
            Change password
          </h2>
          <p className="mt-1 text-sm text-text-muted">
            Changing your password signs you out on your other devices. You stay signed in here.
          </p>
        </div>
        <form noValidate onSubmit={onSubmit} className="space-y-4">
          {formError ? <FormError>{formError}</FormError> : null}
          {done ? (
            <p role="status" className="rounded-lg border border-primary/30 bg-primary-soft p-3 text-sm text-text">
              Your password has been changed. You have been signed out on your other devices.
            </p>
          ) : null}
          <Field
            id="currentPassword"
            name="currentPassword"
            label="Current password"
            type="password"
            autoComplete="current-password"
            error={errors.currentPassword}
            value={values.currentPassword}
            onChange={(e) => set("currentPassword", e.target.value)}
          />
          <Field
            id="newPassword"
            name="newPassword"
            label="New password"
            type="password"
            autoComplete="new-password"
            hint="At least 8 characters."
            error={errors.newPassword}
            value={values.newPassword}
            onChange={(e) => set("newPassword", e.target.value)}
          />
          <Field
            id="confirmPassword"
            name="confirmPassword"
            label="Confirm new password"
            type="password"
            autoComplete="new-password"
            error={errors.confirmPassword}
            value={values.confirmPassword}
            onChange={(e) => set("confirmPassword", e.target.value)}
          />
          <button type="submit" disabled={submitting} className={buttonClass({ variant: "primary" })}>
            {submitting ? "Saving…" : "Change password"}
          </button>
        </form>
      </Card>
    </section>
  );
}
