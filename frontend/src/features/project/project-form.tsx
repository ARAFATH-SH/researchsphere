"use client";

import { type FormEvent, type ReactNode, useEffect, useId, useState } from "react";

import { Field, FormError, rsLegendClass, TextAreaField } from "@/components/ui/field";
import { ChipInput } from "@/features/profile/chip-input";
import { length } from "@/features/profile/validation";
import type { Result } from "@/lib/api/call";

import type { Project } from "./api";
import {
  MAX_ABSTRACT_LENGTH,
  MAX_DESCRIPTION_LENGTH,
  MAX_REQUIRED_SKILLS,
  type ProjectErrors,
  type ProjectValues,
  serverFieldError,
  validateProject,
} from "./validation";

export const emptyProject: ProjectValues = { title: "", description: "", abstract: "", requiredSkills: [] };

// valuesOf is the form's starting values for an existing project.
export function valuesOf(project: Project): ProjectValues {
  return {
    title: project.title,
    description: project.description,
    abstract: project.abstract ?? "",
    requiredSkills: project.requiredSkills.map((s) => s.name),
  };
}

export const createLimitMessage = "You’ve created the most projects allowed for today. Try again tomorrow.";
const genericError = "We couldn’t save the project. Try again.";

type ProjectFormProps = {
  initial: ProjectValues;
  // onSubmit sends the valid values; the form shows what goes wrong.
  onSubmit: (values: ProjectValues) => Promise<Result<Project>>;
  onDone: (project: Project) => void;
  // onDirtyChange says whether the form holds changes.
  onDirtyChange?: (dirty: boolean) => void;
  // actions are the buttons; the one of type submit sends the form. busy disables it.
  actions: (busy: boolean) => ReactNode;
  // note is shown above the buttons.
  note?: ReactNode;
  // abstractId is the abstract field's id, for a dialog that opens with the focus there.
  abstractId?: string;
};

// ProjectForm holds a project's title, description, required skills and paper abstract
// (FR-10.1, FR-10.2) for the Create card and the Edit dialog. It checks the API's limits
// before sending, and shows a field the server refuses under that field.
export function ProjectForm({ initial, onSubmit, onDone, onDirtyChange, actions, note, abstractId }: ProjectFormProps) {
  const id = useId();
  const [values, setValues] = useState(initial);
  const [errors, setErrors] = useState<ProjectErrors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [skillDraft, setSkillDraft] = useState(false);

  const dirty =
    skillDraft ||
    values.title !== initial.title ||
    values.description !== initial.description ||
    values.abstract !== initial.abstract ||
    values.requiredSkills.join("\n") !== initial.requiredSkills.join("\n");
  useEffect(() => onDirtyChange?.(dirty), [dirty, onDirtyChange]);

  function set<K extends keyof ProjectValues>(key: K, value: ProjectValues[K]) {
    setValues((v) => ({ ...v, [key]: value }));
    setErrors((e) => ({ ...e, [key]: undefined }));
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    const found = validateProject(values);
    setErrors(found);
    setFormError(null);
    if (Object.keys(found).length > 0) return;
    setBusy(true);
    const res = await onSubmit(values);
    setBusy(false);
    if (res.ok) {
      onDone(res.data);
      return;
    }
    const fieldError = res.code === "bad_request" ? serverFieldError(res.field) : null;
    if (fieldError) setErrors(fieldError);
    else if (res.code === "rate_limited") setFormError(createLimitMessage);
    else setFormError(genericError);
  }

  return (
    <form noValidate onSubmit={submit} className="flex flex-col gap-3">
      {formError && <FormError look="rs">{formError}</FormError>}
      <Field
        id={`${id}-title`}
        label="Title"
        look="rs"
        required
        error={errors.title}
        value={values.title}
        onChange={(e) => set("title", e.target.value)}
      />
      <TextAreaField
        id={`${id}-description`}
        label="Description"
        look="rs"
        required
        rows={3}
        error={errors.description}
        counter={
          length(values.description.trim()) > MAX_DESCRIPTION_LENGTH - 500
            ? { count: length(values.description.trim()), max: MAX_DESCRIPTION_LENGTH }
            : undefined
        }
        value={values.description}
        onChange={(e) => set("description", e.target.value)}
      />
      <fieldset className="flex flex-col gap-1.5">
        <legend className={rsLegendClass}>Required skills</legend>
        <ChipInput
          kind="skills"
          label="Required skills"
          inputLabel="Add a skill"
          hideInputLabel
          limit={MAX_REQUIRED_SKILLS}
          chip="bg-rs-accent-soft text-rs-heading"
          names={values.requiredSkills}
          emptyText={null}
          error={errors.requiredSkills}
          onDraftChange={setSkillDraft}
          onAdd={(name) => {
            set("requiredSkills", [...values.requiredSkills, name]);
            return null;
          }}
          onRemove={(name) => {
            set(
              "requiredSkills",
              values.requiredSkills.filter((s) => s !== name),
            );
            return null;
          }}
          className="flex flex-col gap-2"
        />
      </fieldset>
      <TextAreaField
        id={abstractId ?? `${id}-abstract`}
        label="Paper abstract (optional)"
        look="rs"
        rows={3}
        placeholder="Paste your abstract"
        error={errors.abstract}
        counter={{ count: length(values.abstract.trim()), max: MAX_ABSTRACT_LENGTH }}
        value={values.abstract}
        onChange={(e) => set("abstract", e.target.value)}
      />
      {note}
      {actions(busy)}
    </form>
  );
}
