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

// Field is a label above an input, with an optional hint and an error in red under it.
// The input points to both with aria-describedby (docs/ui-guidelines.md, Forms).
export function Field({ id, label, hint, error, className = "", ...input }: FieldProps) {
  const describedBy = [error ? `${id}-error` : "", hint ? `${id}-hint` : ""].join(" ").trim();
  return (
    <div className={className}>
      <label htmlFor={id} className="block text-sm font-semibold">
        {label}
      </label>
      <input
        id={id}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy || undefined}
        className={inputClass}
        {...input}
      />
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

// FormError is a banner for an error that belongs to no single field.
export function FormError({ children }: { children: ReactNode }) {
  return (
    <p role="alert" className="rounded-lg border border-danger/30 bg-danger/5 p-3 text-sm text-danger">
      {children}
    </p>
  );
}
