"use client";

import { type FormEvent, useState } from "react";

import { SectionCard } from "@/components/section-layout";
import { rsButtonClass } from "@/components/ui/button";
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
    <SectionCard id="change-password" title="Change password">
      <form noValidate onSubmit={onSubmit} className="flex flex-col gap-3">
        {formError ? <FormError look="rs">{formError}</FormError> : null}
        {done ? (
          <p role="status" className="rounded-rs-md bg-rs-accent-soft px-3 py-2.5 text-rs-body text-rs-heading">
            Your password has been changed. You have been signed out on your other devices.
          </p>
        ) : null}
        <div className="grid grid-cols-[repeat(auto-fit,minmax(200px,1fr))] items-start gap-3">
          <Field
            look="rs"
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
            look="rs"
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
            look="rs"
            id="confirmPassword"
            name="confirmPassword"
            label="Confirm new password"
            type="password"
            autoComplete="new-password"
            error={errors.confirmPassword}
            value={values.confirmPassword}
            onChange={(e) => set("confirmPassword", e.target.value)}
          />
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <button type="submit" disabled={submitting} className={rsButtonClass({ size: "form" })}>
            {submitting ? "Saving…" : "Save password"}
          </button>
          <span className="text-rs-caption text-rs-text-muted">
            Changing your password signs you out on your other devices. You stay signed in here.
          </span>
        </div>
      </form>
    </SectionCard>
  );
}
