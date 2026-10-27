import type { ComponentProps, ReactNode } from "react";

// look "rs" is the design's control (design/screens, UI.3 and UI.6); the default is the older
// style that pages not yet restyled still use.
export type FieldLook = "default" | "rs";

type FieldProps = Omit<ComponentProps<"input">, "id"> & {
  id: string;
  label: string;
  hint?: string;
  error?: string;
  look?: FieldLook;
};

export const inputClass =
  "mt-1 block w-full rounded-lg border border-border bg-surface px-3 py-2.5 text-base text-text " +
  "focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/25 " +
  "aria-invalid:border-danger aria-invalid:focus:ring-danger/25";

// rsControl is what every design control shares: a 1px rs-control border and 6px radius, the
// accent focus ring, a 2px rs-danger border when the value is invalid, and the subtle fill
// with a lighter border when disabled. The padding drops by 1px with the 2px border, so the
// text does not move.
const rsControl =
  "block w-full rounded-control border border-rs-control bg-rs-surface text-rs-body font-normal text-rs-text " +
  "focus:shadow-focus focus:outline-none " +
  "aria-invalid:border-2 aria-invalid:border-rs-danger " +
  "disabled:cursor-not-allowed disabled:border-rs-control-disabled disabled:bg-rs-subtle";

// rsInputClass is the design's 44px text control.
export const rsInputClass = `h-11 px-3 aria-invalid:px-[11px] ${rsControl}`;

// rsSelectClass is the 44px select, with 8px padding as in the design.
export const rsSelectClass = `h-11 px-2 aria-invalid:px-[7px] ${rsControl}`;

// rsTextAreaClass is the text area: 10px by 12px padding, its height set by rows.
export const rsTextAreaClass = `px-3 py-2.5 leading-rs-body aria-invalid:px-[11px] aria-invalid:py-[9px] ${rsControl}`;

const looks: Record<FieldLook, { box: string; label: string; hint: string; error: string }> = {
  default: {
    box: "",
    label: "block text-sm font-semibold",
    hint: "mt-1 text-sm text-text-muted",
    error: "mt-1 text-sm text-danger",
  },
  // The label sits 4px above the control; hint and error are 12px, 6px under it.
  rs: {
    box: "flex flex-col gap-1",
    label: "text-rs-body font-medium text-rs-text",
    hint: "mt-0.5 text-rs-caption text-rs-text-muted",
    error: "mt-0.5 text-rs-caption text-rs-danger",
  },
};

// rsLabelState adds the design's "*" after a required label (CSS only, so it is not part of
// the accessible name; the control's required attribute says it) and mutes the label of a
// disabled control.
function rsLabelState({ required, disabled }: { required?: boolean; disabled?: boolean }) {
  return [required ? "after:content-['*']" : "", disabled ? "text-rs-text-muted" : ""].join(" ");
}

type Described = { id: string; label: string; hint?: ReactNode; error?: string };
type LabelState = { required?: boolean; disabled?: boolean };

// describe gives the aria props that link a control to its hint and error.
function describe({ id, hint, error }: Described) {
  const describedBy = [error ? `${id}-error` : "", hint ? `${id}-hint` : ""].join(" ").trim();
  return { "aria-invalid": error ? true : undefined, "aria-describedby": describedBy || undefined };
}

// Labelled is the label above a control and the hint and error under it.
function Labelled({
  id,
  label,
  hint,
  hintClassName = "",
  error,
  className,
  look = "default",
  required,
  disabled,
  children,
}: Described &
  LabelState & { hintClassName?: string; className: string; look?: FieldLook; children: ReactNode }) {
  const styles = looks[look];
  const labelClass = look === "rs" ? `${styles.label} ${rsLabelState({ required, disabled })}` : styles.label;
  return (
    <div className={[styles.box, className].filter(Boolean).join(" ")}>
      <label htmlFor={id} className={labelClass.trim()}>
        {label}
      </label>
      {children}
      {hint ? (
        <p id={`${id}-hint`} className={`${styles.hint} ${hintClassName}`.trim()}>
          {hint}
        </p>
      ) : null}
      {error ? (
        <p id={`${id}-error`} className={styles.error}>
          {error}
        </p>
      ) : null}
    </div>
  );
}

// Field is a label above an input, with an optional hint and an error in red under it.
// The input points to both with aria-describedby (docs/ui-guidelines.md, Forms).
export function Field({ id, label, hint, error, className = "", look = "default", ...input }: FieldProps) {
  return (
    <Labelled
      id={id}
      label={label}
      hint={hint}
      error={error}
      className={className}
      look={look}
      required={input.required}
      disabled={input.disabled}
    >
      <input
        id={id}
        {...describe({ id, label, hint, error })}
        className={look === "rs" ? rsInputClass : inputClass}
        {...input}
      />
    </Labelled>
  );
}

type TextAreaFieldProps = Omit<ComponentProps<"textarea">, "id"> &
  Omit<Described, "hint"> & {
    hint?: ReactNode;
    look?: FieldLook;
    // counter shows "count / max" under the text area, live for screen readers and red once
    // count is over max. It takes the place of the hint.
    counter?: { count: number; max: number };
  };

// TextAreaField is Field for text of several lines. hint may be live text, such as a counter.
export function TextAreaField({
  id,
  label,
  hint,
  error,
  className = "",
  look = "default",
  counter,
  ...textarea
}: TextAreaFieldProps) {
  const shown = counter ? (
    <span
      aria-live="polite"
      className={counter.count > counter.max ? (look === "rs" ? "text-rs-danger" : "text-danger") : undefined}
    >
      {counter.count} / {counter.max}
    </span>
  ) : (
    hint
  );
  return (
    <Labelled
      id={id}
      label={label}
      hint={shown}
      hintClassName={counter ? "text-right" : ""}
      error={error}
      className={className}
      look={look}
      required={textarea.required}
      disabled={textarea.disabled}
    >
      <textarea
        id={id}
        {...describe({ id, label, hint: shown, error })}
        className={look === "rs" ? rsTextAreaClass : inputClass}
        {...textarea}
      />
    </Labelled>
  );
}

type SelectFieldProps = Omit<ComponentProps<"select">, "id"> & Described & { look?: FieldLook };

// SelectField is Field for a choice from a list.
export function SelectField({
  id,
  label,
  hint,
  error,
  className = "",
  look = "default",
  children,
  ...select
}: SelectFieldProps) {
  return (
    <Labelled
      id={id}
      label={label}
      hint={hint}
      error={error}
      className={className}
      look={look}
      required={select.required}
      disabled={select.disabled}
    >
      <select
        id={id}
        {...describe({ id, label, hint, error })}
        className={look === "rs" ? rsSelectClass : inputClass}
        {...select}
      >
        {children}
      </select>
    </Labelled>
  );
}

type CheckboxFieldProps = Omit<ComponentProps<"input">, "type" | "children"> & { label: string };

// CheckboxField is the design's checkbox row: an 18px box and its label 10px apart, at least
// 36px high so it is easy to hit.
export function CheckboxField({ label, className = "", ...input }: CheckboxFieldProps) {
  return (
    <label
      className={`flex min-h-9 items-center gap-2.5 text-rs-body text-rs-text ${input.disabled ? "text-rs-text-muted" : ""} ${className}`.trim()}
    >
      <input
        type="checkbox"
        className="size-[18px] shrink-0 accent-rs-accent-strong focus-visible:shadow-focus focus-visible:outline-none"
        {...input}
      />
      {label}
    </label>
  );
}

// FieldRow puts fields side by side as the design does (flex: 1 1 200px each); they wrap
// under each other when the row is too narrow, as on a phone.
export function FieldRow({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <div className={`flex flex-wrap items-start gap-3 *:min-w-0 *:flex-[1_1_200px] ${className}`.trim()}>{children}</div>;
}

// FormError is a banner for an error that belongs to no single field. The rs look is the
// design's error notice (rs-danger-soft, as on the auth pages).
export function FormError({ children, look = "default" }: { children: ReactNode; look?: FieldLook }) {
  return (
    <p
      role="alert"
      className={
        look === "rs"
          ? "rounded-rs-md border border-rs-danger-line bg-rs-danger-soft px-3 py-2.5 text-rs-body text-rs-danger-strong"
          : "rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-danger"
      }
    >
      {children}
    </p>
  );
}

// rsLegendClass styles a fieldset legend like an rs field label.
export const rsLegendClass = "mb-1 text-rs-body font-medium text-rs-text";
