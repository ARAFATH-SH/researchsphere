"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { SectionCard } from "@/components/section-layout";
import { rsButtonClass } from "@/components/ui/button";
import { chipClass } from "@/components/ui/chip";
import { FormError } from "@/components/ui/field";
import { PageNav } from "@/components/ui/page-nav";
import { relativeTime } from "@/features/notification/format";

import { listMessageReports, type MessageReport, type MessageReportPage, type ReportStatus } from "./api";
import { outcomeLabels, reasonLabels } from "./format";

type State = { status: "loading" } | { status: "error"; message: string } | { status: "loaded"; data: MessageReportPage };

const tabs: { id: ReportStatus; label: string }[] = [
  { id: "open", label: "Open" },
  { id: "resolved", label: "Resolved" },
];

// AdminReports is the card of reported conversations on the admin page (FR-16.9): open and
// resolved, a page at a time. Reading the list records nothing, as it holds no messages.
export function AdminReports() {
  const [query, setQuery] = useState<{ status: ReportStatus; page: number }>({ status: "open", page: 1 });
  const [state, setState] = useState<State>({ status: "loading" });

  useEffect(() => {
    let current = true;
    void listMessageReports(query.status, query.page).then((res) => {
      if (!current) return;
      setState(res.ok ? { status: "loaded", data: res.data } : { status: "error", message: res.message });
    });
    return () => {
      current = false;
    };
  }, [query]);

  function go(next: { status: ReportStatus; page: number }) {
    setState({ status: "loading" });
    setQuery(next);
  }

  const count = state.status === "loaded" ? ` (${state.data.total})` : "";
  return (
    <SectionCard id="reported-messages" title={`Reported conversations${count}`} header>
      <div role="tablist" aria-label="Report status" className="flex gap-1.5 border-b border-rs-divider px-4 py-2.5">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={query.status === tab.id}
            aria-controls="reported-messages-list"
            onClick={() => query.status !== tab.id && go({ status: tab.id, page: 1 })}
            className={`rounded-rs-pill px-3 py-1 text-rs-body font-medium focus-visible:shadow-focus focus-visible:outline-none ${
              query.status === tab.id ? "bg-rs-accent-soft text-rs-heading" : "text-rs-text hover:bg-rs-subtle"
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>
      <div id="reported-messages-list" role="tabpanel">
        {state.status === "loading" && (
          <p role="status" className="px-4 py-3 text-rs-body text-rs-text-muted">
            Loading reports…
          </p>
        )}
        {state.status === "error" && (
          <div className="px-4 py-3">
            <FormError look="rs">{state.message}</FormError>
          </div>
        )}
        {state.status === "loaded" && state.data.items.length === 0 && (
          <p className="px-4 py-3 text-rs-body text-rs-text-muted">
            {query.status === "open" ? "No open reports." : "No resolved reports."}
          </p>
        )}
        {state.status === "loaded" && state.data.items.length > 0 && (
          <>
            <ul>
              {state.data.items.map((report) => (
                <ReportRow key={report.id} report={report} />
              ))}
            </ul>
            <PageNav
              page={state.data.page}
              pageSize={state.data.pageSize}
              total={state.data.total}
              onPage={(page) => go({ ...query, page })}
            />
          </>
        )}
      </div>
      <p className="border-t border-rs-divider px-4 py-2.5 text-rs-caption text-rs-text-muted">
        Admins can read a conversation only after it has been reported.
      </p>
    </SectionCard>
  );
}

function ReportRow({ report }: { report: MessageReport }) {
  return (
    <li className="flex flex-wrap items-center gap-3 border-b border-rs-divider px-4 py-3 last:border-b-0">
      <span className="min-w-[240px] flex-1 text-rs-body text-rs-text">
        <strong className="font-bold">{report.reporter.fullName}</strong> reported{" "}
        {report.reportedUser ? (
          <>
            {report.messageId ? "a message from" : "a conversation with"}{" "}
            <strong className="font-bold">{report.reportedUser.fullName}</strong>
          </>
        ) : (
          "a whole group"
        )}
        <br />
        <span className="text-rs-caption text-rs-text-muted">
          Reason: {reasonLabels[report.reason]} · <time dateTime={report.createdAt}>{relativeTime(report.createdAt)}</time>
        </span>
      </span>
      {report.outcome && <span className={`${chipClass} bg-rs-neutral text-rs-text`}>{outcomeLabels[report.outcome]}</span>}
      <Link
        href={`/admin/reports/${report.id}`}
        aria-label={`Review the report by ${report.reporter.fullName}`}
        className={rsButtonClass({ variant: "outline", size: "xs" })}
      >
        Review
      </Link>
    </li>
  );
}
