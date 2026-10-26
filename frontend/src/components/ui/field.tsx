import type { ComponentProps, ReactNode } from "react";

type FieldProps = Omit<ComponentProps<"input">, "id"> & {
  id: string;
  label: string;
  hint?: string;
  error?: string;
  // look "rs" is the design's control (design/screens, UI.3); the default is the older style
  // that pages not yet restyled still use.
  look?: FieldLook;
};

export type FieldLook = "default" | "rs";

export const inputClass =
  "mt-1 block w-full rounded-lg border border-border bg-surface px-3 py-2.5 text-base text-text " +
  "focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/25 " +
  "aria-invalid:border-danger aria-invalid:focus:ring-danger/25";

// rsInputClass is the design's 44px control: a 1px rs-control border and 6px radius, the
// accent focus ring, and a 2px rs-danger border when the value is invalid.
export const rsInputClass =
  "block h-11 w-full rounded-control border border-rs-control bg-rs-surface px-3 text-rs-body font-normal text-rs-text " +
  "focus:shadow-focus focus:outline-none " +
  "aria-invalid:border-2 aria-invalid:border-rs-danger aria-invalid:px-[11px]";

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
    label: "text-rs-body font-medium",
    hint: "mt-0.5 text-rs-caption text-rs-text-muted",
    error: "mt-0.5 text-rs-caption text-rs-danger",
  },
};

type Described = { id: string; label: string; hint?: ReactNode; error?: string };

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
  error,
  className,
  look = "default",
  children,
}: Described & { className: string; look?: FieldLook; children: ReactNode }) {
  const styles = looks[look];
  return (
    <div className={[styles.box, className].filter(Boolean).join(" ")}>
      <label htmlFor={id} className={styles.label}>
        {label}
      </label>
      {children}
      {hint ? (
        <p id={`${id}-hint`} className={styles.hint}>
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
    <Labelled id={id} label={label} hint={hint} error={error} className={className} look={look}>
      <input
        id={id}
        {...describe({ id, label, hint, error })}
        className={look === "rs" ? rsInputClass : inputClass}
        {...input}
      />
    </Labelled>
  );
}

type TextAreaFieldProps = Omit<ComponentProps<"textarea">, "id"> & Omit<Described, "hint"> & { hint?: ReactNode };

// TextAreaField is Field for text of several lines. hint may be live text, such as a counter.
export function TextAreaField({ id, label, hint, error, className = "", ...textarea }: TextAreaFieldProps) {
  return (
    <Labelled id={id} label={label} hint={hint} error={error} className={className}>
      <textarea id={id} {...describe({ id, label, hint, error })} className={inputClass} {...textarea} />
    </Labelled>
  );
}

type SelectFieldProps = Omit<ComponentProps<"select">, "id"> & Described;

// SelectField is Field for a choice from a list.
export function SelectField({ id, label, hint, error, className = "", children, ...select }: SelectFieldProps) {
  return (
    <Labelled id={id} label={label} hint={hint} error={error} className={className}>
      <select id={id} {...describe({ id, label, hint, error })} className={inputClass} {...select}>
        {children}
      </select>
    </Labelled>
  );
}

// FormError is a banner for an error that belongs to no single field.
export function FormError({ children }: { children: ReactNode }) {
  return (
    <p role="alert" className="rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-danger">
      {children}
    </p>
  );
}
