"use client";

import { type FormEvent, useMemo, useState } from "react";

import { Dialog } from "@/components/ui/dialog";
import { Field, FormError, SelectField } from "@/components/ui/field";

import { type Profile, updateMyProfile } from "./api";
import { countryOptions, isCountryCode } from "./countries";
import { orNull, profileUpdate } from "./profile-update";
import { SaveButton } from "./save-button";
import {
  fieldForProfileCode,
  genericError,
  type IntroErrors,
  type IntroField,
  type IntroValues,
  validateIntro,
} from "./validation";

type DialogProps = {
  profile: Profile;
  onClose: () => void;
  onSaved: () => void;
  returnFocusId?: string;
};

function initialValues(p: Profile): IntroValues {
  return {
    fullName: p.fullName,
    headline: p.headline ?? "",
    jobTitle: p.jobTitle ?? "",
    institution: p.institution ?? "",
    department: p.department ?? "",
    country: p.country ?? "",
    city: p.city ?? "",
  };
}

// IntroDialog edits the header card: name, headline, where the researcher works and the
// country and city (FR-4.1).
export function IntroDialog({
  profile,
  onClose,
  onSaved,
  returnFocusId,
}: DialogProps) {
  const initial = useMemo(() => initialValues(profile), [profile]);
  const [values, setValues] = useState(initial);
  const [errors, setErrors] = useState<IntroErrors>({});
  const [formError, setFormError] = useState<string>();
  const [saving, setSaving] = useState(false);
  const dirty = (Object.keys(initial) as IntroField[]).some(
    (f) => values[f] !== initial[f],
  );
  const countries = useMemo(() => countryOptions(), []);

  function set(field: IntroField, value: string) {
    setValues({ ...values, [field]: value });
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(undefined);
    const found = validateIntro(values);
    setErrors(found);
    if (Object.keys(found).length > 0) return;

    setSaving(true);
    const res = await updateMyProfile(
      profileUpdate(profile, {
        fullName: values.fullName.trim(),
        headline: orNull(values.headline),
        jobTitle: orNull(values.jobTitle),
        institution: orNull(values.institution),
        department: orNull(values.department),
        country: orNull(values.country),
        city: orNull(values.city),
      }),
    );
    setSaving(false);
    if (res.ok) return onSaved();
    const field = fieldForProfileCode(res.code);
    if (field && field !== "bio") setErrors({ [field]: res.message });
    else setFormError(genericError);
  }

  const text = (
    field: Exclude<IntroField, "country">,
    label: string,
    extra: {
      required?: boolean;
      autoComplete?: string;
      className?: string;
    } = {},
  ) => (
    <Field
      id={`intro-${field}`}
      name={field}
      label={label}
      error={errors[field]}
      value={values[field]}
      onChange={(e) => set(field, e.target.value)}
      {...extra}
    />
  );

  return (
    <Dialog
      title="Edit intro"
      dirty={dirty}
      onClose={onClose}
      returnFocusId={returnFocusId}
    >
      <form noValidate onSubmit={onSubmit} className="space-y-4">
        {formError ? <FormError>{formError}</FormError> : null}
        {text("fullName", "Full name", {
          required: true,
          autoComplete: "name",
        })}
        {text("headline", "Headline")}
        {text("jobTitle", "Job title", { autoComplete: "organization-title" })}
        {text("institution", "Institution", { autoComplete: "organization" })}
        {text("department", "Department")}
        <div className="flex flex-col gap-4 sm:flex-row">
          <SelectField
            id="intro-country"
            name="country"
            label="Country"
            error={errors.country}
            value={values.country}
            onChange={(e) => set("country", e.target.value)}
            className="flex-1"
          >
            <option value="">Not set</option>
            {/* A stored value the list does not know stays selectable, so it can be seen and changed. */}
            {values.country && !isCountryCode(values.country) && (
              <option value={values.country}>{values.country}</option>
            )}
            {countries.map((c) => (
              <option key={c.code} value={c.code}>
                {c.name}
              </option>
            ))}
          </SelectField>
          {text("city", "City", {
            autoComplete: "address-level2",
            className: "flex-1",
          })}
        </div>
        <SaveButton saving={saving} />
      </form>
    </Dialog>
  );
}
