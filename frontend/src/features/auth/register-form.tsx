"use client";

import { useRouter } from "next/navigation";
import { type FormEvent, useState } from "react";

import { buttonClass } from "@/components/ui/button";
import { Field, FormError } from "@/components/ui/field";

import { register } from "./api";
import { savePendingEmail } from "./pending-email";
import {
  type FieldErrors,
  fieldForCode,
  type RegisterField,
  type RegisterValues,
  validateRegistration,
} from "./validation";

const fields: { name: RegisterField; label: string; type: string; autoComplete: string; hint?: string }[] = [
  { name: "fullName", label: "Full name", type: "text", autoComplete: "name" },
  { name: "email", label: "Email", type: "email", autoComplete: "email" },
  { name: "password", label: "Password", type: "password", autoComplete: "new-password", hint: "At least 8 characters." },
  { name: "confirmPassword", label: "Confirm password", type: "password", autoComplete: "new-password" },
];

// RegisterForm lets a visitor register (FR-1.1). Messages appear in red under the field
// they concern, whether the browser or the server found the problem (FR-1.2).
export function RegisterForm() {
  const router = useRouter();
  const [values, setValues] = useState<RegisterValues>({ fullName: "", email: "", password: "", confirmPassword: "" });
  const [errors, setErrors] = useState<FieldErrors>({});
  const [formError, setFormError] = useState<string>();
  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(undefined);
    const found = validateRegistration(values);
    setErrors(found);
    if (Object.keys(found).length > 0) return;

    setSubmitting(true);
    const email = values.email.trim();
    const res = await register({ fullName: values.fullName.trim(), email, password: values.password });
    if (res.ok) {
      savePendingEmail(email);
      router.push("/check-email");
      return;
    }
    setSubmitting(false);
    const field = fieldForCode(res.code);
    if (field) setErrors({ [field]: res.message });
    else setFormError(res.message);
  }

  return (
    <form noValidate onSubmit={onSubmit} className="space-y-4">
      {formError ? <FormError>{formError}</FormError> : null}
      {fields.map((f) => (
        <Field
          key={f.name}
          id={f.name}
          name={f.name}
          label={f.label}
          type={f.type}
          autoComplete={f.autoComplete}
          hint={f.hint}
          error={errors[f.name]}
          value={values[f.name]}
          onChange={(e) => setValues({ ...values, [f.name]: e.target.value })}
        />
      ))}
      <button type="submit" disabled={submitting} className={buttonClass({ variant: "primary", full: true })}>
        {submitting ? "Creating account…" : "Create account"}
      </button>
    </form>
  );
}
