"use client";

import { type FormEvent, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { CheckboxField, Field, FormError, TextAreaField } from "@/components/ui/field";
import { bodyLength } from "@/features/messaging/format";
import type { Result } from "@/lib/api/call";

import {
  type ActionInput,
  dismissMessageReport,
  liftRestriction,
  restoreUser,
  restrictUser,
  suspendUser,
  warnUser,
} from "./api";

export type ActionKind = "dismiss" | "warn" | "restrict" | "suspend" | "lift" | "restore";

const minReason = 10;
const maxText = 1000;
const maxDays: Partial<Record<ActionKind, number>> = { restrict: 90, suspend: 365 };
const defaultDays: Partial<Record<ActionKind, string>> = { restrict: "7", suspend: "30" };
const dayLength = 86_400_000;

function title(kind: ActionKind, name: string): string {
  return {
    dismiss: "Dismiss report",
    warn: `Warn ${name}`,
    restrict: `Restrict messaging for ${name}`,
    suspend: `Suspend ${name}`,
    lift: `Lift the restriction on ${name}`,
    restore: `Restore ${name}`,
  }[kind];
}

const submitLabels: Record<ActionKind, string> = {
  dismiss: "Dismiss report",
  warn: "Warn",
  restrict: "Restrict messaging",
  suspend: "Suspend",
  lift: "Lift restriction",
  restore: "Restore",
};

type ActionDialogProps = {
  kind: ActionKind;
  // name is the user acted on. userId is needed for warn, restrict, suspend and restore.
  name: string;
  userId?: string;
  // reportId resolves that report with the action; dismiss needs it. Lift and restore name none.
  reportId?: string;
  restrictionId?: string;
  onClose: () => void;
  onDone: () => void;
  // onRefused hears that the user is an administrator, which the page did not know.
  onRefused?: () => void;
};

type Run = (input: ActionInput, days: number | null) => Promise<Result<unknown>>;

// run sends the action. It reads a kind's IDs here, so each dialog names only those it has.
function runner({ kind, userId = "", reportId = "", restrictionId = "" }: ActionDialogProps): Run {
  switch (kind) {
    case "dismiss":
      return ({ note }) => dismissMessageReport(reportId, note);
    case "warn":
      return (input) => warnUser(userId, input);
    case "restrict":
      return (input, days) => restrictUser(userId, days ?? 0, input);
    case "suspend":
      return (input, days) =>
        suspendUser(userId, days === null ? null : new Date(Date.now() + days * dayLength).toISOString(), input);
    case "lift":
      return ({ reason, note }) => liftRestriction(restrictionId, { reason, ...(note ? { note } : {}) });
    case "restore":
      return ({ reason, note }) => restoreUser(userId, { reason, ...(note ? { note } : {}) });
  }
}

// ActionDialog is one moderation action (FR-15.5 to FR-15.8): the reason the user will see, with
// a preview of it, and an internal note they never will. A dismissal has only the note, as no
// user is told about it.
export function ActionDialog(props: ActionDialogProps) {
  const { kind, name, reportId, onClose, onDone, onRefused } = props;
  const [reason, setReason] = useState("");
  const [note, setNote] = useState("");
  const [days, setDays] = useState(defaultDays[kind] ?? "");
  const [permanent, setPermanent] = useState(false);
  const [reasonError, setReasonError] = useState<string>();
  const [daysError, setDaysError] = useState<string>();
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const hasReason = kind !== "dismiss";
  const dayLimit = maxDays[kind];
  const reasonLength = bodyLength(reason);
  const noteLength = bodyLength(note);

  // chosenDays is the whole number of days, null when none are needed, undefined when invalid.
  function chosenDays(): number | null | undefined {
    if (!dayLimit || permanent) return null;
    const n = Number(days);
    return Number.isInteger(n) && n >= 1 && n <= dayLimit ? n : undefined;
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const span = chosenDays();
    const badReason = hasReason && reasonLength < minReason;
    setReasonError(badReason ? `Write at least ${minReason} characters.` : undefined);
    setDaysError(span === undefined ? `Enter a whole number from 1 to ${dayLimit}.` : undefined);
    if (badReason || span === undefined || reasonLength > maxText || noteLength > maxText) return;

    setError(null);
    setBusy(true);
    const res = await runner(props)(
      { reason: reason.trim(), ...(noteLength > 0 ? { note: note.trim() } : {}), ...(reportId ? { reportId } : {}) },
      span,
    );
    setBusy(false);
    if (res.ok) {
      onDone();
      return;
    }
    setError(res.message);
    if (res.code === "cannot_moderate_admin") onRefused?.();
  }

  return (
    <Dialog title={title(kind, name)} dirty={reason !== "" || note !== ""} onClose={onClose}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-3.5">
        {error && <FormError look="rs">{error}</FormError>}
        {hasReason && (
          <TextAreaField
            id="action-reason"
            label="Reason shown to the user"
            look="rs"
            rows={4}
            required
            error={reasonError}
            counter={{ count: reasonLength, max: maxText }}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        )}
        {dayLimit && (
          <>
            {kind === "suspend" && (
              <CheckboxField label="Permanently" checked={permanent} onChange={(e) => setPermanent(e.target.checked)} />
            )}
            {!permanent && (
              <Field
                id="action-days"
                label={`Days (1 to ${dayLimit})`}
                look="rs"
                type="number"
                inputMode="numeric"
                min={1}
                max={dayLimit}
                required
                error={daysError}
                value={days}
                onChange={(e) => setDays(e.target.value)}
              />
            )}
          </>
        )}
        <TextAreaField
          id="action-note"
          label="Internal note (optional)"
          look="rs"
          rows={2}
          counter={{ count: noteLength, max: maxText }}
          value={note}
          onChange={(e) => setNote(e.target.value)}
        />
        {hasReason && reasonLength > 0 && (
          <p className="text-rs-caption break-words whitespace-pre-wrap text-rs-text-muted">The user will see: {reason.trim()}</p>
        )}
        <div className="flex flex-wrap justify-end gap-2 pt-1">
          <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
            Cancel
          </button>
          <button type="submit" disabled={busy} className={rsButtonClass({ variant: kind === "suspend" ? "destructive" : "primary", size: "form" })}>
            {submitLabels[kind]}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
