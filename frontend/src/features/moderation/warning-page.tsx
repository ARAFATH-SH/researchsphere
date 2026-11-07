"use client";

import { useEffect, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { FormError } from "@/components/ui/field";
import { dateOnly } from "@/lib/date-time";

import { type AccountWarning, acknowledgeWarning, getMyWarning } from "./api";
import { refreshPendingWarnings } from "./pending-warnings";

type State = { status: "loading" } | { status: "notFound" } | { status: "error" } | { status: "ok"; warning: AccountWarning };

const cardClass = "flex flex-col gap-3 rounded-card border border-rs-border bg-rs-surface p-5 shadow-card";

// WarningPage is /warnings/[id], where the account_warning notification and the banner lead: the
// reason, the date and "I understand" until it is acknowledged (FR-15.5).
export function WarningPage({ warningId }: { warningId: string }) {
  const [state, setState] = useState<State>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let current = true;
    void getMyWarning(warningId).then((res) => {
      if (!current) return;
      if (res.ok) setState({ status: "ok", warning: res.data });
      else setState({ status: res.code === "not_found" ? "notFound" : "error" });
    });
    return () => {
      current = false;
    };
  }, [warningId, attempt]);

  async function acknowledge() {
    setBusy(true);
    setFailed(false);
    const res = await acknowledgeWarning(warningId);
    if (res.ok) {
      // Read it again: the server keeps the time.
      const next = await getMyWarning(warningId);
      if (next.ok) setState({ status: "ok", warning: next.data });
      void refreshPendingWarnings();
    } else {
      setFailed(true);
    }
    setBusy(false);
  }

  if (state.status === "loading") {
    return (
      <p role="status" className="text-rs-body text-rs-text-muted">
        Loading…
      </p>
    );
  }
  if (state.status === "notFound") return <p className="text-rs-body text-rs-text-muted">This warning isn&apos;t available.</p>;
  if (state.status === "error") {
    return (
      <div className="flex flex-col items-start gap-2">
        <FormError look="rs">Couldn&apos;t load this warning.</FormError>
        <button type="button" onClick={() => setAttempt((n) => n + 1)} className={rsButtonClass({ variant: "secondary", size: "sm" })}>
          Try again
        </button>
      </div>
    );
  }
  const { warning } = state;
  return (
    <section aria-labelledby="warning-title" className={cardClass}>
      <h1 id="warning-title" className="text-rs-lead leading-rs-snug font-medium text-rs-text">
        A warning from ResearchSphere
      </h1>
      <p className="text-rs-caption text-rs-text-muted">{dateOnly(warning.createdAt)}</p>
      <p className="text-rs-body whitespace-pre-wrap text-rs-text">{warning.reason}</p>
      {warning.acknowledgedAt ? (
        <p role="status" className="text-rs-body text-rs-text-muted">
          Acknowledged on {dateOnly(warning.acknowledgedAt)}
        </p>
      ) : (
        <>
          {failed && <FormError look="rs">Couldn&apos;t save that. Try again.</FormError>}
          <button type="button" disabled={busy} onClick={acknowledge} className={`self-start ${rsButtonClass({ size: "form" })}`}>
            I understand
          </button>
        </>
      )}
    </section>
  );
}
