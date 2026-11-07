"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { SectionCard } from "@/components/section-layout";
import { rsButtonClass } from "@/components/ui/button";
import { chipClass } from "@/components/ui/chip";
import { FormError } from "@/components/ui/field";
import { PageNav } from "@/components/ui/page-nav";
import { useSession } from "@/features/auth/session";
import { dateAndTime } from "@/lib/date-time";

import { ActionDialog, type ActionKind } from "./action-dialog";
import { getUserModeration, type ModerationAction, type ModerationHistory } from "./api";
import { actionLabels, isActiveRestriction, outcomeLabels, reasonLabels } from "./format";

type State = { status: "loading" } | { status: "notFound" } | { status: "error"; message: string } | { status: "ok"; data: ModerationHistory };

type Open = { kind: Exclude<ActionKind, "dismiss" | "lift"> } | { kind: "lift"; restrictionId: string };

const linkClass = "text-rs-accent-strong hover:underline focus-visible:shadow-focus focus-visible:outline-none";

// UserModeration is /admin/users/[id]: the account, what can be done to it, and its history
// (FR-15.9). An administrator cannot act on themselves or on another administrator (FR-15.10):
// the buttons are left out, and a refusal from the server hides them too.
export function UserModeration({ userId }: { userId: string }) {
  const session = useSession();
  const me = session.status === "signedIn" ? session.user.id : null;
  const [query, setQuery] = useState({ page: 1, attempt: 0 });
  const [state, setState] = useState<State>({ status: "loading" });
  const [open, setOpen] = useState<Open | null>(null);
  const [refused, setRefused] = useState(false);

  useEffect(() => {
    let current = true;
    void getUserModeration(userId, query.page).then((res) => {
      if (!current) return;
      if (res.ok) setState({ status: "ok", data: res.data });
      else if (res.code === "not_found") setState({ status: "notFound" });
      else setState({ status: "error", message: res.message });
    });
    return () => {
      current = false;
    };
  }, [userId, query]);

  if (state.status === "loading") {
    return (
      <p role="status" className="text-rs-body text-rs-text-muted">
        Loading…
      </p>
    );
  }
  if (state.status === "notFound") return <p className="text-rs-body text-rs-text-muted">This user doesn&apos;t exist.</p>;
  if (state.status === "error") return <FormError look="rs">{state.message}</FormError>;

  const { user, items, page, pageSize, total } = state.data;
  const canAct = !refused && user.role !== "admin" && user.id !== me;
  const activeRestriction = items.some((a) => isActiveRestriction(a));

  function done() {
    setOpen(null);
    setQuery((q) => ({ page: 1, attempt: q.attempt + 1 }));
  }

  return (
    <div className="flex flex-col gap-4">
      <Link href="/admin#users" className={`text-rs-body ${linkClass}`}>
        Back to users
      </Link>
      <section aria-labelledby="moderated-user" className="flex flex-col gap-3 rounded-card border border-rs-border bg-rs-surface p-5 shadow-card">
        <div className="flex flex-wrap items-center gap-2">
          <h1 id="moderated-user" className="text-rs-lead leading-rs-snug font-medium text-rs-text">
            {user.fullName}
          </h1>
          {user.role === "admin" ? (
            <span className={`${chipClass} bg-rs-violet-soft text-rs-violet`}>Admin</span>
          ) : (
            <span className={`${chipClass} bg-rs-neutral text-rs-text`}>Researcher</span>
          )}
          {user.suspended ? (
            <span className={`${chipClass} bg-rs-danger-soft text-rs-danger-strong`}>Suspended</span>
          ) : (
            <span className={`${chipClass} bg-rs-accent-soft text-rs-heading`}>Active</span>
          )}
        </div>
        <p className="text-rs-body break-all text-rs-text-muted">{user.email}</p>
        {user.suspended && (
          <p className="text-rs-body text-rs-text">
            {user.suspendedUntil ? `Suspended until ${dateAndTime(user.suspendedUntil)}.` : "Suspended permanently."}
            {user.suspensionReason ? ` Reason: ${user.suspensionReason}` : ""}
          </p>
        )}
        {canAct && (
          <div className="flex flex-wrap gap-2">
            <ActionButton label="Warn" onClick={() => setOpen({ kind: "warn" })} />
            {!activeRestriction && <ActionButton label="Restrict messaging" onClick={() => setOpen({ kind: "restrict" })} />}
            {user.suspended ? (
              <ActionButton label="Restore" onClick={() => setOpen({ kind: "restore" })} />
            ) : (
              <ActionButton label="Suspend" danger onClick={() => setOpen({ kind: "suspend" })} />
            )}
          </div>
        )}
      </section>

      <SectionCard id="moderation-history" title="Moderation history" header>
        {items.length === 0 ? (
          <p className="px-4 py-3 text-rs-body text-rs-text-muted">No moderation actions.</p>
        ) : (
          <>
            <ul>
              {items.map((action) => (
                <HistoryRow
                  key={action.id}
                  action={action}
                  onLift={
                    canAct && isActiveRestriction(action) && action.restrictionId
                      ? () => setOpen({ kind: "lift", restrictionId: action.restrictionId as string })
                      : undefined
                  }
                />
              ))}
            </ul>
            <PageNav
              page={page}
              pageSize={pageSize}
              total={total}
              onPage={(next) => {
                setState({ status: "loading" });
                setQuery((q) => ({ ...q, page: next }));
              }}
            />
          </>
        )}
      </SectionCard>

      {open && (
        <ActionDialog
          kind={open.kind}
          name={user.fullName}
          userId={user.id}
          restrictionId={open.kind === "lift" ? open.restrictionId : undefined}
          onClose={() => setOpen(null)}
          onDone={done}
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

function detailLine(action: ModerationAction): string | null {
  if (action.action === "warning") {
    return action.acknowledgedAt ? `Acknowledged on ${dateAndTime(action.acknowledgedAt)}` : "Not acknowledged yet";
  }
  if (action.action === "restriction") {
    if (action.liftedAt) return `Lifted on ${dateAndTime(action.liftedAt)}`;
    return action.endsAt ? `Until ${dateAndTime(action.endsAt)}` : null;
  }
  if (action.action === "suspension") return action.endsAt ? `Until ${dateAndTime(action.endsAt)}` : "Permanent";
  return null;
}

function HistoryRow({ action, onLift }: { action: ModerationAction; onLift?: () => void }) {
  const detail = detailLine(action);
  return (
    <li className="flex flex-wrap items-start gap-3 border-b border-rs-divider px-4 py-3 last:border-b-0">
      <div className="min-w-[240px] flex-1 text-rs-body text-rs-text">
        <p>
          <strong className="font-bold">{actionLabels[action.action]}</strong>
          <span className="text-rs-caption text-rs-text-muted">
            {" · "}
            {dateAndTime(action.createdAt)}
            {action.adminName ? ` · by ${action.adminName}` : ""}
          </span>
        </p>
        <p className="break-words whitespace-pre-wrap">{action.reason}</p>
        {detail && <p className="text-rs-caption text-rs-text-muted">{detail}</p>}
        {action.note && <p className="text-rs-caption break-words whitespace-pre-wrap text-rs-text-muted">Internal note: {action.note}</p>}
        {action.report && (
          <p className="text-rs-caption">
            <Link href={`/admin/reports/${action.report.id}`} className={linkClass}>
              Report: {reasonLabels[action.report.reason]}, {outcomeLabels[action.report.outcome]}
            </Link>
          </p>
        )}
      </div>
      {onLift && (
        <button type="button" onClick={onLift} className={rsButtonClass({ variant: "secondary", size: "xs" })}>
          Lift
        </button>
      )}
    </li>
  );
}
