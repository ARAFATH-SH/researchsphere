import type { ComponentProps, ReactNode } from "react";

type FieldProps = Omit<ComponentProps<"input">, "id"> & {
  id: string;
  label: string;
  hint?: string;
  error?: string;
};

export const inputClass =
  "mt-1 block w-full rounded-lg border border-border bg-surface px-3 py-2.5 text-base text-text " +
  "focus:border-primary focus:outline-none focus:ring-2 focus:ring-primary/25 " +
  "aria-invalid:border-danger aria-invalid:focus:ring-danger/25";

type Described = { id: string; label: string; hint?: ReactNode; error?: string };

// describe gives the aria props that link a control to its hint and error.
function describe({ id, hint, error }: Described) {
  const describedBy = [error ? `${id}-error` : "", hint ? `${id}-hint` : ""].join(" ").trim();
  return { "aria-invalid": error ? true : undefined, "aria-describedby": describedBy || undefined };
}

// Labelled is the label above a control and the hint and error under it.
function Labelled({ id, label, hint, error, className, children }: Described & { className: string; children: ReactNode }) {
  return (
    <div className={className}>
      <label htmlFor={id} className="block text-sm font-semibold">
        {label}
      </label>
      {children}
      {hint ? (
        <p id={`${id}-hint`} className="mt-1 text-sm text-text-muted">
          {hint}
        </p>
      ) : null}
      {error ? (
        <p id={`${id}-error`} className="mt-1 text-sm text-danger">
          {error}
        </p>
      ) : null}
    </div>
  );
}

// Field is a label above an input, with an optional hint and an error in red under it.
// The input points to both with aria-describedby (docs/ui-guidelines.md, Forms).
export function Field({ id, label, hint, error, className = "", ...input }: FieldProps) {
  return (
    <Labelled id={id} label={label} hint={hint} error={error} className={className}>
      <input id={id} {...describe({ id, label, hint, error })} className={inputClass} {...input} />
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
