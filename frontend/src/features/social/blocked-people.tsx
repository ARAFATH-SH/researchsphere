"use client";

import { useState } from "react";

import { Avatar } from "@/components/avatar";
import { SectionCard } from "@/components/section-layout";
import { rsButtonClass } from "@/components/ui/button";
import { FormError } from "@/components/ui/field";

import { type BlockedPerson, listBlockedPeople } from "./api";
import { BlockDialog } from "./block-dialog";
import { usePagedList } from "./paged-list";

// BlockedPeople is the settings card "Messaging & email" (design/screens/Settings.html). Only
// its Blocked people list exists: the digest toggle needs FR-16.11, which has no API yet.
export function BlockedPeople() {
  const list = usePagedList<BlockedPerson>("blocked-people", listBlockedPeople, (p) => p.userId);
  const [unblocking, setUnblocking] = useState<BlockedPerson | null>(null);
  const { state } = list;

  return (
    <SectionCard id="messaging-email" title="Messaging & email">
      <div className="flex flex-col gap-2">
        <h3 className="text-rs-body font-bold text-rs-text">Blocked people</h3>
        {state.status === "loading" && (
          <p role="status" className="text-rs-body text-rs-text-muted">
            Loading…
          </p>
        )}
        {(state.status === "error" || state.status === "notFound") && (
          <div className="flex flex-col items-start gap-2">
            <FormError look="rs">Couldn&apos;t load the people you blocked.</FormError>
            <button type="button" onClick={list.retry} className={rsButtonClass({ variant: "secondary", size: "xs" })}>
              Try again
            </button>
          </div>
        )}
        {state.status === "ok" && state.items.length === 0 && (
          <p className="text-rs-body text-rs-text-muted">You haven&apos;t blocked anyone.</p>
        )}
        {state.status === "ok" && state.items.length > 0 && (
          <ul className="flex flex-col gap-2">
            {state.items.map((person) => (
              <li key={person.userId} className="flex flex-wrap items-center gap-2.5">
                <Avatar name={person.fullName} src={person.photoUrl} size="bar" decorative />
                <span className="min-w-0 flex-1 text-rs-body text-rs-text">{person.fullName}</span>
                <button
                  type="button"
                  aria-label={`Unblock ${person.fullName}`}
                  onClick={() => setUnblocking(person)}
                  className={rsButtonClass({ variant: "secondary", size: "xs" })}
                >
                  Unblock
                </button>
              </li>
            ))}
          </ul>
        )}
        {state.status === "ok" && state.moreFailed && <FormError look="rs">Couldn&apos;t load more people.</FormError>}
        {state.status === "ok" && state.items.length < state.total && (
          <button
            type="button"
            disabled={state.loadingMore}
            onClick={list.loadMore}
            className={`self-start ${rsButtonClass({ variant: "secondary", size: "xs" })}`}
          >
            {state.loadingMore ? "Loading…" : "Show more"}
          </button>
        )}
      </div>
      {unblocking && (
        <BlockDialog
          action="unblock"
          userId={unblocking.userId}
          name={unblocking.fullName}
          onClose={() => setUnblocking(null)}
          onDone={() => {
            list.remove(unblocking.userId);
            setUnblocking(null);
          }}
        />
      )}
    </SectionCard>
  );
}
