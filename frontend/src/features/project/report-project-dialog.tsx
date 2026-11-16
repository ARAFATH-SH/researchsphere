"use client";

import { type FormEvent, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { FormError, SelectField, TextAreaField } from "@/components/ui/field";
import { bodyLength } from "@/features/messaging/format";
import { maxReportDetails, reportReasons } from "@/features/messaging/report-dialog";

import { type ReportReason, reportProject } from "./api";

const errorText: Record<string, string> = {
  already_reported: "You’ve already reported this project. Our team is reviewing it.",
  cannot_report_own_project: "You can’t report your own project.",
  account_suspended: "Your account is suspended, so you can’t send reports.",
  not_found: "This project is no longer available.",
};

// ReportProjectDialog reports a project to the administrators (FR-15.11), with the same
// reason picker as message reports.
export function ReportProjectDialog({
  projectId,
  onClose,
  returnFocusId,
}: {
  projectId: string;
  onClose: () => void;
  returnFocusId?: string;
}) {
  const [reason, setReason] = useState<ReportReason | "">("");
  const [details, setDetails] = useState("");
  const [reasonError, setReasonError] = useState<string>();
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState(false);
  const length = bodyLength(details);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!reason) {
      setReasonError("Choose a reason.");
      return;
    }
    if (length > maxReportDetails || busy) return;
    setReasonError(undefined);
    setError(null);
    setBusy(true);
    const res = await reportProject(projectId, { reason, ...(length > 0 ? { details: details.trim() } : {}) });
    setBusy(false);
    if (res.ok) setSent(true);
    else setError(errorText[res.code] ?? "We couldn’t send the report. Try again.");
  }

  if (sent) {
    return (
      <Dialog title="Report project" onClose={onClose} returnFocusId={returnFocusId}>
        <div className="flex flex-col gap-3">
          <p role="status" className="text-rs-body text-rs-text">
            Thanks. Our team will review it.
          </p>
          <div className="flex justify-end pt-1">
            <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
              Done
            </button>
          </div>
        </div>
      </Dialog>
    );
  }

  return (
    <Dialog
      title="Report project"
      dirty={reason !== "" || details !== ""}
      onClose={onClose}
      returnFocusId={returnFocusId}
    >
      <form noValidate onSubmit={submit} className="flex flex-col gap-3.5">
        {error && <FormError look="rs">{error}</FormError>}
        <SelectField
          id="report-project-reason"
          label="Reason"
          look="rs"
          required
          error={reasonError}
          value={reason}
          onChange={(e) => setReason(e.target.value as ReportReason | "")}
        >
          <option value="">Choose a reason</option>
          {reportReasons.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </SelectField>
        <TextAreaField
          id="report-project-details"
          label="Details (optional)"
          look="rs"
          rows={4}
          counter={{ count: length, max: maxReportDetails }}
          value={details}
          onChange={(e) => setDetails(e.target.value)}
        />
        <div className="flex flex-wrap justify-end gap-2 pt-1">
          <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
            Cancel
          </button>
          <button type="submit" disabled={busy || length > maxReportDetails} className={rsButtonClass({ size: "form" })}>
            Send report
          </button>
        </div>
      </form>
    </Dialog>
  );
}
