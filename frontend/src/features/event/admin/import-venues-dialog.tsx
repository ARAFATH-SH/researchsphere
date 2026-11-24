"use client";

import { type FormEvent, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { chipClass } from "@/components/ui/chip";
import { Dialog } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/field";

import { importVenues, type VenueImportReason, type VenueImportResult, type VenueImportRow } from "../api";

type Props = {
  onClose: () => void;
  onImported: (result: VenueImportResult) => void;
  returnFocusId?: string;
};

// reasonWords says why a row is refused, in words (codes of VenueImportReason).
const reasonWords: Record<string, string> = {
  required: "is required",
  too_long: "is too long",
  too_short: "is too short",
  invalid: "is not valid",
  not_https: "must start with https://",
  unknown_field: "names a research field that is not on the list",
  duplicate_field: "names a research field twice",
  too_many_fields: "has more than 5 research fields",
  not_allowed: "does not apply to this type of venue or attendance",
  invalid_time_zone: "is not AoE, UTC or a zone such as Asia/Dhaka",
  invalid_date: "is not a valid date",
  ends_before_starts: "ends before it starts",
  after_deadline: "is after the submission deadline",
  before_deadline: "is before the submission deadline",
  duplicate_in_file: "repeats an earlier row's key",
  venue_removed: "matches a removed venue, which is not brought back",
  deadline_tbd: "has no deadline yet",
  unknown_country: "names an unknown country",
  unreadable: "could not be read",
};

// reasonText is one refusal: "submissionDeadline is required", or the row's own fault.
export function reasonText(r: VenueImportReason): string {
  const words = reasonWords[r.code] ?? `(${r.code})`;
  return r.field === "row" ? `The row ${words}` : `${r.field} ${words}`;
}

const outcomeChip: Record<VenueImportRow["outcome"], string> = {
  created: "bg-rs-accent-soft text-rs-heading",
  updated: "bg-rs-violet-soft text-rs-violet",
  unchanged: "bg-rs-neutral text-rs-text",
  rejected: "bg-rs-danger-soft text-rs-danger-strong",
};

const outcomeLabel: Record<VenueImportRow["outcome"], string> = {
  created: "New",
  updated: "Updated",
  unchanged: "Unchanged",
  rejected: "Rejected",
};

// importSummary is the result in one sentence.
export function importSummary(r: VenueImportResult): string {
  const n = (count: number, one: string, many: string) => `${count} ${count === 1 ? one : many}`;
  return `${n(r.created, "new venue", "new venues")}, ${r.updated} updated, ${r.unchanged} unchanged, ${r.rejected} rejected.`;
}

// ImportVenuesDialog imports venues from a CSV or JSON file (FR-14.5; docs/venues-import.md) in
// two steps: a dry run shows what each row would do, then Confirm import writes the accepted rows.
export function ImportVenuesDialog({ onClose, onImported, returnFocusId }: Props) {
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<VenueImportResult | null>(null);
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);

  function refused(res: { code: string; message: string }) {
    if (res.code === "file_too_large") setFailure("The file is larger than 1 MiB.");
    else if (res.code === "too_many_rows") setFailure("The file has more than 1,000 rows.");
    else if (res.code === "invalid_import_file") setFailure(`The file cannot be read: ${res.message}`);
    else setFailure(res.message);
  }

  async function check(event: FormEvent) {
    event.preventDefault();
    if (!file) {
      setFailure("Choose a CSV or JSON file.");
      return;
    }
    setBusy(true);
    setFailure("");
    const res = await importVenues(file, true);
    setBusy(false);
    if (res.ok) setPreview(res.data);
    else refused(res);
  }

  async function confirm() {
    if (!file) return;
    setBusy(true);
    setFailure("");
    const res = await importVenues(file, false);
    setBusy(false);
    if (res.ok) onImported(res.data);
    else refused(res);
  }

  const accepted = preview ? preview.created + preview.updated : 0;
  return (
    <Dialog title="Import venues from a file" dirty={file !== null} onClose={onClose} returnFocusId={returnFocusId}>
      <div className="flex flex-col gap-4">
        <form noValidate onSubmit={check} className="flex flex-col gap-3">
          <div className="flex flex-col gap-1">
            <label htmlFor="venue-import-file" className="text-rs-body font-medium text-rs-text">
              CSV or JSON file
            </label>
            <input
              id="venue-import-file"
              type="file"
              accept=".csv,.json,text/csv,application/json"
              aria-describedby="venue-import-hint"
              onChange={(e) => {
                setFile(e.target.files?.[0] ?? null);
                setPreview(null);
                setFailure("");
              }}
              className="text-rs-body file:mr-3 file:rounded-rs-pill file:border file:border-rs-control file:bg-rs-surface file:px-3 file:py-1.5 file:text-rs-body file:font-medium"
            />
            <p id="venue-import-hint" className="text-rs-caption text-rs-text-muted">
              At most 1 MiB and 1,000 rows. The columns are described in docs/venues-import.md.
            </p>
          </div>
          {!preview && (
            <button type="submit" disabled={busy || !file} className={`${rsButtonClass({ size: "form" })} self-start`}>
              {busy ? "Checking…" : "Check file (dry run)"}
            </button>
          )}
        </form>
        {failure && <FormError look="rs">{failure}</FormError>}
        {preview && (
          <>
            <p role="status" className="text-rs-body text-rs-text">
              Dry run, nothing changed yet: {importSummary(preview)}
            </p>
            <div className="max-h-80 overflow-auto rounded-card border border-rs-border">
              <table className="w-full min-w-[480px] border-collapse text-left text-rs-body">
                <caption className="sr-only">What each row of the file would do</caption>
                <thead className="sticky top-0 bg-rs-surface text-rs-caption text-rs-text-muted">
                  <tr>
                    {["Row", "Key", "Outcome", "Reasons"].map((h) => (
                      <th key={h} scope="col" className="px-2 py-2 font-medium first:pl-3 last:pr-3">
                        {h}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {preview.rows.map((r) => (
                    <tr key={r.row} className="border-t border-rs-divider align-top">
                      <td className="px-2 py-2 pl-3">{r.row}</td>
                      <td className="px-2 py-2 break-all">{r.sourceKey || "—"}</td>
                      <td className="px-2 py-2">
                        <span className={`${chipClass} ${outcomeChip[r.outcome]}`}>{outcomeLabel[r.outcome]}</span>
                      </td>
                      <td className="px-2 py-2 pr-3">
                        {r.reasons.length > 0 && (
                          <ul className="list-disc pl-4">
                            {r.reasons.map((reason, i) => (
                              <li key={i}>{reasonText(reason)}</li>
                            ))}
                          </ul>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="flex flex-wrap justify-end gap-2">
              <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
                Cancel
              </button>
              <button type="button" onClick={confirm} disabled={busy || accepted === 0} className={rsButtonClass({ size: "form" })}>
                {busy ? "Importing…" : accepted === 0 ? "Nothing to import" : `Confirm import (${accepted})`}
              </button>
            </div>
          </>
        )}
      </div>
    </Dialog>
  );
}
