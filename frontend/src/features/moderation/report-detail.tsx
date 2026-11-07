"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { Avatar } from "@/components/avatar";
import { SectionCard } from "@/components/section-layout";
import { rsButtonClass } from "@/components/ui/button";
import { chipClass } from "@/components/ui/chip";
import { FormError } from "@/components/ui/field";
import { useSession } from "@/features/auth/session";
import { MessageText } from "@/features/messaging/message-text";
import { dateAndTime } from "@/lib/date-time";

import { ActionDialog, type ActionKind } from "./action-dialog";
import {
  getMessageReport,
  type MessageReport,
  type MessageReportPerson,
  type ReportedMessage,
} from "./api";
import { outcomeLabels, reasonLabels } from "./format";

const linkClass = "text-rs-accent-strong hover:underline focus-visible:shadow-focus focus-visible:outline-none";

type Loaded = {
  report: MessageReport;
  // messages are oldest first, as they read.
  messages: ReportedMessage[];
  nextBefore: string | null;
  loadingEarlier: boolean;
  earlierFailed: boolean;
};

type State = { status: "loading" } | { status: "notFound" } | { status: "error"; message: string } | ({ status: "ok" } & Loaded);

// ReportDetail is /admin/reports/[id]: the report, the two people, the conversation up to the
// report, read-only (NFR-14), and the actions on an open report. Each read of a page of messages
// is recorded on the server, so an action changes the page without reading it again.
export function ReportDetail({ reportId }: { reportId: string }) {
  const session = useSession();
  const me = session.status === "signedIn" ? session.user.id : null;
  const [state, setState] = useState<State>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  const [dialog, setDialog] = useState<ActionKind | null>(null);
  const [refused, setRefused] = useState(false);

  useEffect(() => {
    let current = true;
    void getMessageReport(reportId).then((res) => {
      if (!current) return;
      if (res.ok) {
        setState({
          status: "ok",
          report: res.data.report,
          messages: [...res.data.messages].reverse(),
          nextBefore: res.data.nextBefore,
          loadingEarlier: false,
          earlierFailed: false,
        });
      } else if (res.code === "not_found") {
        setState({ status: "notFound" });
      } else {
        setState({ status: "error", message: res.message });
      }
    });
    return () => {
      current = false;
    };
  }, [reportId, attempt]);

  async function loadEarlier() {
    if (state.status !== "ok" || !state.nextBefore || state.loadingEarlier) return;
    setState({ ...state, loadingEarlier: true, earlierFailed: false });
    const res = await getMessageReport(reportId, state.nextBefore);
    setState((now) => {
      if (now.status !== "ok") return now;
      if (!res.ok) return { ...now, loadingEarlier: false, earlierFailed: true };
      return {
        ...now,
        messages: [...[...res.data.messages].reverse(), ...now.messages],
        nextBefore: res.data.nextBefore,
        loadingEarlier: false,
      };
    });
  }

  function resolved(outcome: NonNullable<MessageReport["outcome"]>) {
    setDialog(null);
    setState((now) =>
      now.status === "ok"
        ? { ...now, report: { ...now.report, status: "resolved", outcome, resolvedAt: new Date().toISOString() } }
        : now,
    );
  }

  const back = (
    <Link href="/admin#reported-messages" className={`text-rs-body ${linkClass}`}>
      Back to reports
    </Link>
  );
  if (state.status === "loading") {
    return (
      <p role="status" className="text-rs-body text-rs-text-muted">
        Loading report…
      </p>
    );
  }
  if (state.status === "notFound") {
    return (
      <div className="flex flex-col items-start gap-2">
        <p className="text-rs-body text-rs-text-muted">This report isn&apos;t available.</p>
        {back}
      </div>
    );
  }
  if (state.status === "error") {
    return (
      <div className="flex flex-col items-start gap-2">
        <FormError look="rs">{state.message}</FormError>
        <button type="button" onClick={() => setAttempt((n) => n + 1)} className={rsButtonClass({ variant: "secondary", size: "sm" })}>
          Try again
        </button>
      </div>
    );
  }

  const { report, messages } = state;
  const target = report.reportedUser;
  // An administrator cannot be moderated, oneself included (FR-15.10). The report can still be
  // dismissed.
  const canAct = !refused && target.role !== "admin" && target.userId !== me;
  return (
    <div className="flex flex-col gap-4">
      {back}
      <h1 className="text-rs-lead leading-rs-snug font-medium text-rs-text">Report by {report.reporter.fullName}</h1>
      <SectionCard id="report-info" title="Report">
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-rs-body text-rs-text">
          <dt className="text-rs-text-muted">Reason</dt>
          <dd>{reasonLabels[report.reason]}</dd>
          <dt className="text-rs-text-muted">Reported</dt>
          <dd>{dateAndTime(report.createdAt)}</dd>
          <dt className="text-rs-text-muted">About</dt>
          <dd>{report.messageId ? "One message" : "The conversation"}</dd>
          <dt className="text-rs-text-muted">Status</dt>
          <dd>
            {report.outcome ? (
              <span className={`${chipClass} bg-rs-neutral text-rs-text`}>{outcomeLabels[report.outcome]}</span>
            ) : (
              "Open"
            )}
          </dd>
          {report.details && (
            <>
              <dt className="text-rs-text-muted">Details</dt>
              <dd className="break-words whitespace-pre-wrap">{report.details}</dd>
            </>
          )}
          {report.note && (
            <>
              <dt className="text-rs-text-muted">Note</dt>
              <dd className="break-words whitespace-pre-wrap">{report.note}</dd>
            </>
          )}
        </dl>
        <div className="flex flex-wrap gap-6 border-t border-rs-divider pt-3">
          <Person label="Reporter" person={report.reporter} />
          <Person label="Reported user" person={target} />
        </div>
      </SectionCard>

      {report.status === "open" && (
        <SectionCard id="report-actions" title="Actions">
          <div className="flex flex-wrap gap-2">
            <ActionButton label="Dismiss report" onClick={() => setDialog("dismiss")} />
            {canAct && (
              <>
                <ActionButton label="Warn" onClick={() => setDialog("warn")} />
                <ActionButton label="Restrict messaging" onClick={() => setDialog("restrict")} />
                <ActionButton label="Suspend" danger onClick={() => setDialog("suspend")} />
              </>
            )}
          </div>
          {!canAct && <p className="text-rs-caption text-rs-text-muted">Administrators can&apos;t be warned, restricted or suspended.</p>}
        </SectionCard>
      )}

      <SectionCard id="report-messages" title="Conversation" header>
        <p role="note" className="border-b border-rs-divider px-4 py-2.5 text-rs-caption text-rs-text-muted">
          Messages after the report are not shown.
        </p>
        <ol aria-label="Messages up to the report" className="flex flex-col gap-3 p-4">
          {state.nextBefore && (
            <li className="flex flex-col items-center gap-1">
              {state.earlierFailed && (
                <p role="alert" className="text-rs-caption text-rs-danger">
                  Couldn&apos;t load earlier messages.
                </p>
              )}
              <button
                type="button"
                disabled={state.loadingEarlier}
                onClick={loadEarlier}
                className={rsButtonClass({ variant: "secondary", size: "xs" })}
              >
                {state.loadingEarlier ? "Loading…" : "Load earlier"}
              </button>
            </li>
          )}
          {messages.length === 0 && <li className="text-rs-body text-rs-text-muted">No messages.</li>}
          {messages.map((m) => (
            <Message key={m.id} message={m} report={report} />
          ))}
        </ol>
      </SectionCard>

      {dialog && (
        <ActionDialog
          kind={dialog}
          name={target.fullName}
          userId={target.userId}
          reportId={report.id}
          onClose={() => setDialog(null)}
          onDone={() => resolved(dialog === "dismiss" ? "dismissed" : "action_taken")}
          onRefused={() => setRefused(true)}
        />
      )}
    </div>
  );
}

function ActionButton({ label, danger = false, onClick }: { label: string; danger?: boolean; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className={rsButtonClass({ variant: danger ? "danger" : "secondary", size: "sm" })}>
      {label}
    </button>
  );
}

function Person({ label, person }: { label: string; person: MessageReportPerson }) {
  return (
    <div className="flex min-w-0 items-center gap-2.5">
      <Avatar name={person.fullName} src={person.photoUrl} size="bar" decorative />
      <div className="min-w-0 text-rs-body">
        <p className="text-rs-caption text-rs-text-muted">{label}</p>
        <Link href={`/profile/${person.userId}`} className={`block truncate font-medium ${linkClass}`}>
          {person.fullName}
        </Link>
        <Link
          href={`/admin/users/${person.userId}`}
          aria-label={`Moderation history of ${person.fullName}`}
          className={`text-rs-caption ${linkClass}`}
        >
          Moderation history
        </Link>
      </div>
    </div>
  );
}

function Message({ message, report }: { message: ReportedMessage; report: MessageReport }) {
  const sender = message.senderId === report.reporter.userId ? report.reporter : report.reportedUser;
  const reported = message.id === report.messageId;
  return (
    <li
      data-reported={reported || undefined}
      className={`rounded-rs-md px-3 py-2 text-rs-body text-rs-text ${
        reported ? "border border-rs-warning-line bg-rs-warning-soft" : "bg-rs-subtle"
      }`}
    >
      <p className="text-rs-caption text-rs-text-muted">
        <strong className="font-medium text-rs-text">{sender.fullName}</strong> · {dateAndTime(message.createdAt)}
        {reported && " · Reported message"}
      </p>
      <MessageText body={message.body} />
    </li>
  );
}
