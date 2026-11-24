"use client";

import { type FormEvent, useId, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { chipClass } from "@/components/ui/chip";
import { Dialog } from "@/components/ui/dialog";
import { CheckboxField, Field, FieldRow, FormError, rsInputClass, SelectField, TextAreaField } from "@/components/ui/field";

import { createVenue, type ResearchField, updateVenue, type Venue, type VenueType } from "../api";
import { attendanceLabels, countryName, sourceLabels, syncedSources, typeLabels, venueTypes } from "../labels";
import {
  emptyForm,
  type FormErrors,
  type FormField,
  formFieldOrder,
  formOf,
  knownZones,
  localTime,
  maxFields,
  maxScope,
  shows,
  toInput,
  toPatch,
  validate,
  type VenueForm,
} from "./venue-validation";

type Props = {
  // venue is the venue to edit, with its scope; absent to add one.
  venue?: Venue;
  fields: ResearchField[];
  onClose: () => void;
  onSaved: (venue: Venue) => void;
  returnFocusId?: string;
};

// VenueFormDialog adds or edits a venue (FR-15.1) with the rules of VenueInput: attendance and
// event dates only for conferences and workshops, a place only in person or hybrid, rolling only
// for journals (then no zone and no deadline), a time zone of AoE, UTC or an IANA name, 1 to 5
// fields, an https link and a scope of at most 10,000 characters. An edit sends only what
// changed. Editing a venue of the automatic import locks it, and the dialog says so first.
export function VenueFormDialog({ venue, fields, onClose, onSaved, returnFocusId }: Props) {
  const id = useId();
  const start = venue ? formOf(venue) : emptyForm;
  const [form, setForm] = useState<VenueForm>(start);
  const [errors, setErrors] = useState<FormErrors>({});
  const [failure, setFailure] = useState("");
  const [saving, setSaving] = useState(false);
  const show = shows(form);
  const dirty = JSON.stringify(form) !== JSON.stringify(start);
  const synced = venue ? syncedSources.includes(venue.source) : false;

  function set<K extends FormField>(key: K, value: VenueForm[K]) {
    setForm((f) => ({ ...f, [key]: value }));
    if (errors[key]) setErrors((e) => ({ ...e, [key]: undefined }));
  }

  function focusFirst(errs: FormErrors) {
    const first = formFieldOrder.find((k) => errs[k]);
    if (first) document.getElementById(`${id}-${first}`)?.focus();
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    const errs = validate(form);
    setErrors(errs);
    setFailure("");
    if (Object.keys(errs).length > 0) {
      focusFirst(errs);
      return;
    }
    if (venue) {
      const patch = toPatch(venue, form);
      if (Object.keys(patch).length === 0) {
        onClose();
        return;
      }
      setSaving(true);
      const res = await updateVenue(venue.id, patch);
      setSaving(false);
      if (res.ok) onSaved(res.data);
      else refused(res);
      return;
    }
    setSaving(true);
    const res = await createVenue(toInput(form));
    setSaving(false);
    if (res.ok) onSaved(res.data);
    else refused(res);
  }

  function refused(res: { code: string; message: string; field?: string }) {
    if (res.code === "bad_request" && res.field && (formFieldOrder as string[]).includes(res.field)) {
      const errs = { [res.field]: res.message } as FormErrors;
      setErrors(errs);
      focusFirst(errs);
    } else if (res.code === "not_found") {
      setFailure("This venue has been removed.");
    } else {
      setFailure(res.message);
    }
  }

  const err = (k: FormField) => errors[k];
  const fieldId = (k: FormField) => `${id}-${k}`;
  const zones = knownZones();

  return (
    <Dialog title={venue ? `Edit ${venue.name}` : "Add a venue"} dirty={dirty} onClose={onClose} returnFocusId={returnFocusId}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-4">
        {venue && synced && (
          <div className="flex flex-col gap-1 rounded-card border border-rs-warning-line bg-rs-warning-soft px-3 py-2.5 text-rs-body text-rs-warning">
            <span className="flex flex-wrap items-center gap-2 font-medium">
              Imported from {sourceLabels[venue.source]}
              {venue.locked && <span className={`${chipClass} bg-rs-surface text-rs-warning`}>Locked</span>}
            </span>
            <span>
              {venue.locked
                ? "An administrator has edited this venue, so the automatic import no longer updates it."
                : "Saving a change locks this venue: the automatic import will stop updating it."}
            </span>
          </div>
        )}
        <Field
          look="rs"
          id={fieldId("name")}
          label="Name"
          required
          value={form.name}
          error={err("name")}
          onChange={(e) => set("name", e.target.value)}
        />
        <FieldRow>
          <Field
            look="rs"
            id={fieldId("shortName")}
            label="Short name"
            hint="The acronym, such as ICML."
            value={form.shortName}
            error={err("shortName")}
            onChange={(e) => set("shortName", e.target.value)}
          />
          <SelectField
            look="rs"
            id={fieldId("type")}
            label="Type"
            required
            value={form.type}
            onChange={(e) => set("type", e.target.value as VenueType)}
          >
            {venueTypes.map((t) => (
              <option key={t} value={t}>
                {typeLabels[t]}
              </option>
            ))}
          </SelectField>
        </FieldRow>
        {show.rolling && (
          <CheckboxField
            id={fieldId("rolling")}
            label="Open submission (rolling): no deadline"
            checked={form.rolling}
            onChange={(e) => set("rolling", e.target.checked)}
          />
        )}
        <FieldPicker
          id={fieldId("fields")}
          fields={fields}
          selected={form.fields}
          error={err("fields")}
          onChange={(next) => set("fields", next)}
        />
        {show.attendance && (
          <SelectField
            look="rs"
            id={fieldId("attendance")}
            label="Attendance"
            required
            value={form.attendance}
            error={err("attendance")}
            onChange={(e) => set("attendance", e.target.value as VenueForm["attendance"])}
          >
            <option value="">Choose…</option>
            {(Object.keys(attendanceLabels) as (keyof typeof attendanceLabels)[]).map((a) => (
              <option key={a} value={a}>
                {attendanceLabels[a]}
              </option>
            ))}
          </SelectField>
        )}
        {show.place && (
          <FieldRow>
            <Field
              look="rs"
              id={fieldId("city")}
              label="City"
              required
              value={form.city}
              error={err("city")}
              onChange={(e) => set("city", e.target.value)}
            />
            <Field
              look="rs"
              id={fieldId("country")}
              label="Country code"
              required
              maxLength={2}
              autoCapitalize="characters"
              hint={/^[A-Za-z]{2}$/.test(form.country) ? countryName(form.country) : "Two letters, such as BD."}
              value={form.country}
              error={err("country")}
              onChange={(e) => set("country", e.target.value.toUpperCase())}
            />
          </FieldRow>
        )}
        {show.dates && (
          <FieldRow>
            <Field
              look="rs"
              id={fieldId("startsOn")}
              label="First day"
              type="date"
              value={form.startsOn}
              error={err("startsOn")}
              onChange={(e) => set("startsOn", e.target.value)}
            />
            <Field
              look="rs"
              id={fieldId("endsOn")}
              label="Last day"
              type="date"
              min={form.startsOn || undefined}
              value={form.endsOn}
              error={err("endsOn")}
              onChange={(e) => set("endsOn", e.target.value)}
            />
          </FieldRow>
        )}
        {show.deadlines && (
          <>
            <div className="flex flex-col gap-1">
              <label htmlFor={fieldId("timeZone")} className="text-rs-body font-medium text-rs-text after:content-['*']">
                Time zone of the deadlines
              </label>
              <input
                id={fieldId("timeZone")}
                list={`${id}-zones`}
                required
                autoComplete="off"
                className={rsInputClass}
                value={form.timeZone}
                aria-invalid={err("timeZone") ? true : undefined}
                aria-describedby={`${id}-tz-hint${err("timeZone") ? ` ${fieldId("timeZone")}-error` : ""}`}
                onChange={(e) => set("timeZone", e.target.value)}
              />
              <datalist id={`${id}-zones`}>
                {zones.map((z) => (
                  <option key={z} value={z}>
                    {z === "AoE" ? "Anywhere on Earth (UTC−12)" : undefined}
                  </option>
                ))}
              </datalist>
              <p id={`${id}-tz-hint`} className="mt-0.5 text-rs-caption text-rs-text-muted">
                AoE (anywhere on Earth, UTC−12), UTC, or a zone such as Asia/Dhaka. Deadlines are the local times the call for
                papers gives.
              </p>
              {err("timeZone") && (
                <p id={`${fieldId("timeZone")}-error`} className="mt-0.5 text-rs-caption text-rs-danger">
                  {err("timeZone")}
                </p>
              )}
            </div>
            <Field
              look="rs"
              id={fieldId("submissionDeadline")}
              label="Submission deadline"
              type="datetime-local"
              step={1}
              required
              value={form.submissionDeadline}
              error={err("submissionDeadline")}
              onChange={(e) => set("submissionDeadline", localTime(e.target.value))}
            />
            <FieldRow>
              <Field
                look="rs"
                id={fieldId("abstractDeadline")}
                label="Abstract deadline"
                type="datetime-local"
                step={1}
                value={form.abstractDeadline}
                error={err("abstractDeadline")}
                onChange={(e) => set("abstractDeadline", localTime(e.target.value))}
              />
              <Field
                look="rs"
                id={fieldId("notificationAt")}
                label="Notification"
                type="datetime-local"
                step={1}
                value={form.notificationAt}
                error={err("notificationAt")}
                onChange={(e) => set("notificationAt", localTime(e.target.value))}
              />
            </FieldRow>
          </>
        )}
        <Field
          look="rs"
          id={fieldId("officialUrl")}
          label="Official link"
          type="url"
          required
          inputMode="url"
          hint="An https:// link to the call for papers."
          value={form.officialUrl}
          error={err("officialUrl")}
          onChange={(e) => set("officialUrl", e.target.value)}
        />
        <TextAreaField
          look="rs"
          id={fieldId("scope")}
          label="Scope"
          required
          rows={8}
          counter={{ count: form.scope.length, max: maxScope }}
          value={form.scope}
          error={err("scope")}
          onChange={(e) => set("scope", e.target.value)}
        />
        {failure && <FormError look="rs">{failure}</FormError>}
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
            Cancel
          </button>
          <button type="submit" disabled={saving} className={rsButtonClass({ size: "form" })}>
            {saving ? "Saving…" : venue ? "Save changes" : "Add venue"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}

type PickerProps = { id: string; fields: ResearchField[]; selected: string[]; error?: string; onChange: (next: string[]) => void };

// FieldPicker chooses 1 to 5 research fields; once 5 are chosen the others are disabled.
function FieldPicker({ id, fields, selected, error, onChange }: PickerProps) {
  const full = selected.length >= maxFields;
  function toggle(key: string, on: boolean) {
    const next = on ? [...selected, key] : selected.filter((k) => k !== key);
    onChange(fields.map((f) => f.key).filter((k) => next.includes(k)));
  }
  return (
    <fieldset aria-describedby={`${id}-count${error ? ` ${id}-error` : ""}`} aria-invalid={error ? true : undefined}>
      <legend className="text-rs-body font-medium text-rs-text after:content-['*']">Research fields</legend>
      {/* The first checkbox takes the id, so an error can focus the group. */}
      <div className="mt-1 grid grid-cols-2 gap-x-3 max-[520px]:grid-cols-1">
        {fields.map((f, i) => {
          const checked = selected.includes(f.key);
          return (
            <CheckboxField
              key={f.key}
              id={i === 0 ? id : undefined}
              label={f.label}
              checked={checked}
              disabled={!checked && full}
              onChange={(e) => toggle(f.key, e.target.checked)}
            />
          );
        })}
      </div>
      <p id={`${id}-count`} className="mt-0.5 text-rs-caption text-rs-text-muted">
        {selected.length} of at most {maxFields} chosen.
      </p>
      {error && (
        <p id={`${id}-error`} className="mt-0.5 text-rs-caption text-rs-danger">
          {error}
        </p>
      )}
    </fieldset>
  );
}
