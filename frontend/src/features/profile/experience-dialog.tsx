"use client";

import { type FormEvent, useMemo, useState } from "react";

import { Dialog } from "@/components/ui/dialog";
import {
  CheckboxField,
  Field,
  FieldRow,
  FormError,
  rsLegendClass,
  rsSelectClass,
  SelectField,
  TextAreaField,
} from "@/components/ui/field";

import {
  addExperience,
  type EmploymentType,
  type Experience,
  type ExperienceCreate,
  type LocationType,
  removeExperience,
  updateExperience,
} from "./api";
import {
  currentMonth,
  employmentTypeLabels,
  locationTypeLabels,
  monthNames,
} from "./format";
import { orNull } from "./profile-update";
import { FormActions } from "./save-button";
import {
  type ExperienceErrors,
  type ExperienceValues,
  experienceMessages,
  fieldForExperienceCode,
  genericError,
  length,
  MAX_EXPERIENCE_DESCRIPTION_LENGTH,
  MIN_EXPERIENCE_YEAR,
  validateExperience,
} from "./validation";

type ExperienceDialogProps = {
  // entry is the entry to edit; without it the dialog adds a new one.
  entry?: Experience;
  onClose: () => void;
  // onSaved closes the dialog and reloads the profile; so does onDeleted.
  onSaved: () => void;
  onDeleted: () => void;
  returnFocusId?: string;
};

function valuesOf(e?: Experience): ExperienceValues {
  return {
    title: e?.title ?? "",
    employmentType: e?.employmentType ?? "",
    organization: e?.organization ?? "",
    location: e?.location ?? "",
    locationType: e?.locationType ?? "",
    description: e?.description ?? "",
    startMonth: e ? String(e.startDate.month) : "",
    startYear: e ? String(e.startDate.year) : "",
    endMonth: e?.endDate ? String(e.endDate.month) : "",
    endYear: e?.endDate ? String(e.endDate.year) : "",
    current: e?.isCurrent ?? false,
  };
}

// bodyOf builds the request from valid values. A current role sends endDate null, also
// when an existing past role becomes current (FR-4.8; the server never clears it itself).
function bodyOf(v: ExperienceValues): ExperienceCreate {
  return {
    title: v.title.trim(),
    organization: v.organization.trim(),
    employmentType: v.employmentType as EmploymentType,
    location: orNull(v.location),
    locationType:
      v.locationType === "" ? null : (v.locationType as LocationType),
    startDate: { year: Number(v.startYear), month: Number(v.startMonth) },
    endDate: v.current
      ? null
      : { year: Number(v.endYear), month: Number(v.endMonth) },
    isCurrent: v.current,
    description: orNull(v.description),
  };
}

// ExperienceDialog adds or edits one work experience entry, and deletes it after a
// confirm step inside the dialog (FR-4.8).
export function ExperienceDialog({
  entry,
  onClose,
  onSaved,
  onDeleted,
  returnFocusId,
}: ExperienceDialogProps) {
  const initial = useMemo(() => valuesOf(entry), [entry]);
  const [values, setValues] = useState(initial);
  const [errors, setErrors] = useState<ExperienceErrors>({});
  const [formError, setFormError] = useState<string>();
  const [saving, setSaving] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const dirty = (Object.keys(initial) as (keyof ExperienceValues)[]).some(
    (k) => values[k] !== initial[k],
  );
  const count = length(values.description.trim());

  function set(
    field: Exclude<keyof ExperienceValues, "current">,
    value: string,
  ) {
    setValues({ ...values, [field]: value });
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(undefined);
    const found = validateExperience(values);
    setErrors(found);
    if (Object.keys(found).length > 0) return;

    const body = bodyOf(values);
    setSaving(true);
    const res = entry
      ? await updateExperience(entry.id, body)
      : await addExperience(body);
    setSaving(false);
    if (res.ok) return onSaved();
    const field = fieldForExperienceCode(res.code);
    if (field) setErrors({ [field]: res.message });
    else
      setFormError(
        res.code === "experience_limit_reached"
          ? experienceMessages.limitReached
          : genericError,
      );
  }

  async function onDelete() {
    if (!entry) return;
    setFormError(undefined);
    setDeleting(true);
    const res = await removeExperience(entry.id);
    setDeleting(false);
    if (res.ok || res.code === "not_found") return onDeleted();
    setConfirmingDelete(false);
    setFormError(genericError);
  }

  const text = (
    field: "title" | "organization" | "location",
    label: string,
    required = false,
  ) => (
    <Field
      id={`experience-${field}`}
      name={field}
      label={label}
      look="rs"
      required={required}
      error={errors[field]}
      value={values[field]}
      onChange={(e) => set(field, e.target.value)}
    />
  );

  return (
    <Dialog
      title={entry ? "Edit experience" : "Add experience"}
      dirty={dirty}
      onClose={onClose}
      returnFocusId={returnFocusId}
    >
      <form noValidate onSubmit={onSubmit} className="space-y-3">
        {formError ? <FormError look="rs">{formError}</FormError> : null}
        {text("title", "Title", true)}
        <SelectField
          id="experience-employmentType"
          name="employmentType"
          label="Employment type"
          look="rs"
          required
          error={errors.employmentType}
          value={values.employmentType}
          onChange={(e) => set("employmentType", e.target.value)}
        >
          <option value="">Please select</option>
          {Object.entries(employmentTypeLabels).map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </SelectField>
        {text("organization", "Organization", true)}
        <CheckboxField
          name="current"
          label="I am currently working in this role"
          checked={values.current}
          onChange={(e) => {
            // A current role has no end date, so ticking the box clears it.
            setValues({
              ...values,
              current: e.target.checked,
              endMonth: "",
              endYear: "",
            });
            if (e.target.checked) setErrors({ ...errors, endDate: undefined });
          }}
        />
        <MonthField
          which="start"
          label="Start date"
          month={values.startMonth}
          year={values.startYear}
          error={errors.startDate}
          onMonth={(v) => set("startMonth", v)}
          onYear={(v) => set("startYear", v)}
        />
        {!values.current && (
          <MonthField
            which="end"
            label="End date"
            month={values.endMonth}
            year={values.endYear}
            error={errors.endDate}
            onMonth={(v) => set("endMonth", v)}
            onYear={(v) => set("endYear", v)}
          />
        )}
        <FieldRow>
          {text("location", "Location (optional)")}
          <SelectField
            id="experience-locationType"
            name="locationType"
            label="Location type (optional)"
            look="rs"
            error={errors.locationType}
            value={values.locationType}
            onChange={(e) => set("locationType", e.target.value)}
          >
            <option value="">Please select</option>
            {Object.entries(locationTypeLabels).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </SelectField>
        </FieldRow>
        <TextAreaField
          id="experience-description"
          name="description"
          label="Description (optional)"
          look="rs"
          rows={6}
          error={errors.description}
          value={values.description}
          onChange={(e) => set("description", e.target.value)}
          counter={{ count, max: MAX_EXPERIENCE_DESCRIPTION_LENGTH }}
        />
        <FormActions
          saving={saving}
          remove={
            entry && {
              label: "Delete experience",
              question: "Delete this experience? This cannot be undone.",
              confirming: confirmingDelete,
              deleting,
              onAsk: () => setConfirmingDelete(true),
              onCancel: () => setConfirmingDelete(false),
              onConfirm: () => void onDelete(),
            }
          }
        />
      </form>
    </Dialog>
  );
}

type MonthFieldProps = {
  which: "start" | "end";
  label: string;
  month: string;
  year: string;
  error?: string;
  onMonth: (value: string) => void;
  onYear: (value: string) => void;
};

// MonthField is a month and a year select under one legend ("Start date"). The years run
// from this year back to 1950; the error, such as a future month, is shown once under both.
function MonthField({
  which,
  label,
  month,
  year,
  error,
  onMonth,
  onYear,
}: MonthFieldProps) {
  const errorId = `experience-${which}-error`;
  const latest = currentMonth().year;
  const years = Array.from(
    { length: latest - MIN_EXPERIENCE_YEAR + 1 },
    (_, i) => latest - i,
  );
  const control = {
    "aria-invalid": error ? true : undefined,
    "aria-describedby": error ? errorId : undefined,
  };
  return (
    <fieldset>
      <legend className={rsLegendClass}>{label}</legend>
      <FieldRow>
        <select
          aria-label={`${label}: month`}
          name={`${which}Month`}
          value={month}
          onChange={(e) => onMonth(e.target.value)}
          className={rsSelectClass}
          {...control}
        >
          <option value="">Month</option>
          {monthNames.map((name, i) => (
            <option key={name} value={String(i + 1)}>
              {name}
            </option>
          ))}
        </select>
        <select
          aria-label={`${label}: year`}
          name={`${which}Year`}
          value={year}
          onChange={(e) => onYear(e.target.value)}
          className={rsSelectClass}
          {...control}
        >
          <option value="">Year</option>
          {years.map((y) => (
            <option key={y} value={String(y)}>
              {y}
            </option>
          ))}
        </select>
      </FieldRow>
      {error ? (
        <p id={errorId} className="mt-1.5 text-rs-caption text-rs-danger">
          {error}
        </p>
      ) : null}
    </fieldset>
  );
}
