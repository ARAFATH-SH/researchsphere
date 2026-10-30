"use client";

import { type FormEvent, type ReactNode, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Field, FormError } from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";

import { publicationError } from "./publication-errors";
import { venueYear } from "./publication-item";
import {
  addFromDOI,
  lookupDOI,
  type PubFailure,
  type PublicationPreview,
} from "./publications-api";

// excerpt shortens text to about max characters, at a word boundary, with an ellipsis.
export function excerpt(text: string, max = 300): string {
  if (text.length <= max) return text;
  const cut = text.slice(0, max);
  const space = cut.lastIndexOf(" ");
  return `${(space > max / 2 ? cut.slice(0, space) : cut).trimEnd()}…`;
}

// Unavailable is the FR-5.6 notice: the outside service failed. It shows the API's message
// and, when given, a button to add the publication by hand instead.
export function Unavailable({
  message,
  action,
}: {
  message: string;
  action?: ReactNode;
}) {
  return (
    <div
      role="alert"
      className="flex flex-col items-start gap-2 rounded-rs-md border border-rs-warning-line bg-rs-warning-soft px-3 py-2.5 text-rs-body text-rs-warning"
    >
      <p>{message}</p>
      {action}
    </div>
  );
}

// DoiTab is "Add by DOI" (FR-5.2): the user looks the DOI up, checks the preview and
// confirms. A DOI already on the user's profile cannot be confirmed; one stored for another
// author is linked to the same record (FR-5.4). When OpenAlex cannot be reached, onManual
// opens "Add manually" with the DOI filled in (FR-5.6).
export function DoiTab({
  onAdded,
  onManual,
}: {
  onAdded: (linked: boolean) => void;
  // onManual gets the DOI and the API's message about the failure.
  onManual: (doi: string, message: string) => void;
}) {
  const [doi, setDoi] = useState("");
  const [preview, setPreview] = useState<PublicationPreview | null>(null);
  const [doiError, setDoiError] = useState<string>();
  const [formError, setFormError] = useState<string>();
  const [unavailable, setUnavailable] = useState<string>();
  const [busy, setBusy] = useState<"lookup" | "add" | null>(null);

  function clear() {
    setDoiError(undefined);
    setFormError(undefined);
    setUnavailable(undefined);
  }

  function fail(res: PubFailure) {
    if (res.code === "metadata_unavailable") return setUnavailable(res.message);
    const { field, message } = publicationError(res);
    if (field === "doi") setDoiError(message);
    else setFormError(message);
  }

  async function lookUp(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    clear();
    setPreview(null);
    if (doi.trim() === "") return setDoiError("Enter a DOI.");
    setBusy("lookup");
    const res = await lookupDOI(doi.trim());
    setBusy(null);
    if (res.ok) setPreview(res.data);
    else fail(res);
  }

  async function confirm() {
    if (!preview) return;
    clear();
    setBusy("add");
    const res = await addFromDOI(preview.doi);
    setBusy(null);
    if (res.ok) onAdded(res.data.linked);
    else fail(res);
  }

  const meta = preview && venueYear(preview);
  return (
    <div className="space-y-3">
      <form noValidate onSubmit={lookUp} className="flex flex-wrap items-start gap-2">
        <Field
          id="publication-doi"
          label="DOI"
          look="rs"
          className="min-w-0 flex-[1_1_280px]"
          placeholder="10.1145/3292500.3330701"
          error={doiError}
          value={doi}
          onChange={(e) => {
            setDoi(e.target.value);
            setPreview(null);
            clear();
          }}
        />
        {/* The label row is 24px high; the button lines up with the input under it. */}
        <button
          type="submit"
          disabled={busy !== null}
          className={`mt-6 ${rsButtonClass({ size: "md" })}`}
        >
          {busy === "lookup" && <Spinner />}
          {busy === "lookup" ? "Looking up…" : "Look up"}
        </button>
      </form>
      {formError && <FormError look="rs">{formError}</FormError>}
      {unavailable && (
        <Unavailable
          message={unavailable}
          action={
            <button
              type="button"
              onClick={() => onManual(preview?.doi ?? doi.trim(), unavailable)}
              className={rsButtonClass({ variant: "secondary", size: "sm" })}
            >
              Add manually instead
            </button>
          }
        />
      )}
      {preview && (
        <section
          aria-label="Publication found"
          className="flex flex-col gap-1.5 rounded-rs-md border border-rs-accent-strong bg-rs-surface p-3.5"
        >
          <p className="text-rs-caption font-medium text-rs-heading">
            {preview.existsAlready
              ? "Found on ResearchSphere — check before saving"
              : "Found on OpenAlex — check before saving"}
          </p>
          <p className="text-rs-lead font-bold break-words text-rs-text">
            {preview.title}
          </p>
          {meta && <p className="text-rs-body text-rs-text">{meta}</p>}
          {preview.abstract && (
            <p className="text-rs-body text-rs-text">
              Abstract: {excerpt(preview.abstract)}
            </p>
          )}
          {preview.linkedToMe ? (
            <p className="rounded-control bg-rs-subtle px-2.5 py-2 text-rs-caption text-rs-text">
              This publication is already on your profile.
            </p>
          ) : (
            preview.existsAlready && (
              <p className="rounded-control bg-rs-accent-soft px-2.5 py-2 text-rs-caption text-rs-heading">
                This publication is already on ResearchSphere; you&apos;ll be
                added as an author.
              </p>
            )
          )}
          <div className="flex flex-wrap gap-2 pt-1">
            <button
              type="button"
              onClick={confirm}
              disabled={preview.linkedToMe || busy !== null}
              className={rsButtonClass({ size: "md" })}
            >
              {busy === "add" && <Spinner />}
              {busy === "add" ? "Adding…" : "Confirm & add"}
            </button>
            <button
              type="button"
              onClick={() => {
                setPreview(null);
                clear();
              }}
              disabled={busy !== null}
              className={rsButtonClass({ variant: "secondary", size: "md" })}
            >
              Cancel
            </button>
          </div>
        </section>
      )}
    </div>
  );
}
