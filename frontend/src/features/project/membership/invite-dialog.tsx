"use client";

import { type ReactNode, useEffect, useId, useState } from "react";

import { Avatar } from "@/components/avatar";
import { Dialog } from "@/components/ui/dialog";
import { Field, FormError } from "@/components/ui/field";
import { useRecommendations } from "@/features/recommendation/recommendations-provider";
import { type PersonSearchResult, searchPeople } from "@/features/search/api";
import type { Result } from "@/lib/api/call";

import type { MemberRole, ProjectPerson } from "../api";
import { InviteControl } from "./invite-button";
import type { InviteState } from "./use-membership";

// searchDelay is how long the invite search waits after the last key before it asks.
export const searchDelay = 300;

// minSearchLength is the shortest search the invite dialog sends.
export const minSearchLength = 2;

// quickPicks is how many recommended researchers the dialog offers under the search.
export const quickPicks = 3;

type Search = { q: string; res: Result<PersonSearchResult[]> };

// InviteDialog is the owner's "+ Invite" (FR-11.3): a researcher search by keyword, a role
// picker on each result, and the project's top recommended researchers as quick picks. Members,
// invited researchers and those asking to join show where they stand instead of Invite.
export function InviteDialog({
  inviteState,
  onInvite,
  onClose,
  returnFocusId,
}: {
  inviteState: (userId: string) => InviteState;
  onInvite: (person: ProjectPerson, role: MemberRole) => Promise<boolean>;
  onClose: () => void;
  returnFocusId?: string;
}) {
  const searchId = useId();
  const recs = useRecommendations();
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState<Search | null>(null);
  const q = query.trim();

  useEffect(() => {
    if (q.length < minSearchLength) return;
    let current = true;
    const timer = setTimeout(() => {
      void searchPeople({ q, institution: "", interests: [], page: 1, keywordOnly: true }).then((res) => {
        if (current) setSearch({ q, res: res.ok ? { ok: true, data: res.data.items } : res });
      });
    }, searchDelay);
    return () => {
      current = false;
      clearTimeout(timer);
    };
  }, [q]);

  const shown = search?.q === q && q.length >= minSearchLength ? search.res : null;
  const picks = recs?.state.status === "ready" ? recs.state.rows.slice(0, quickPicks) : [];

  let results: ReactNode = null;
  if (q.length >= minSearchLength) {
    if (!shown)
      results = (
        <p role="status" className="text-rs-body text-rs-text-muted">
          Searching…
        </p>
      );
    else if (!shown.ok) results = <FormError look="rs">We couldn’t search right now. Try again.</FormError>;
    else if (shown.data.length === 0) results = <p className="text-rs-body text-rs-text-muted">No researchers match “{q}”.</p>;
    else
      results = (
        <PeopleList label="Search results" people={shown.data} inviteState={inviteState} onInvite={onInvite} />
      );
  }

  return (
    <Dialog title="Invite to this project" onClose={onClose} returnFocusId={returnFocusId}>
      <div className="flex flex-col gap-4">
        <Field
          id={searchId}
          look="rs"
          type="search"
          label="Search researchers"
          hint="By name, institution or interest."
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          autoComplete="off"
        />
        {results}
        {picks.length > 0 && (
          <div className="flex flex-col gap-2">
            <h3 className="text-rs-body font-medium text-rs-text">Suggested for this project</h3>
            <PeopleList label="Suggested researchers" people={picks} inviteState={inviteState} onInvite={onInvite} />
          </div>
        )}
      </div>
    </Dialog>
  );
}

type Person = ProjectPerson & { institution?: string | null };

function PeopleList({
  label,
  people,
  inviteState,
  onInvite,
}: {
  label: string;
  people: Person[];
  inviteState: (userId: string) => InviteState;
  onInvite: (person: ProjectPerson, role: MemberRole) => Promise<boolean>;
}) {
  return (
    <ul aria-label={label} className="flex flex-col">
      {people.map((p) => {
        const person: ProjectPerson = { userId: p.userId, fullName: p.fullName, headline: p.headline, photoUrl: p.photoUrl };
        const subtitle = p.headline ?? p.institution;
        return (
          <li key={p.userId} className="flex flex-wrap items-center gap-3 border-t border-rs-divider py-2.5 first:border-t-0">
            <Avatar name={p.fullName} src={p.photoUrl} size="thread" decorative />
            <span className="flex min-w-[160px] flex-1 flex-col">
              <span className="text-rs-body font-bold text-rs-text">{p.fullName}</span>
              {subtitle && <span className="text-rs-caption text-rs-text-muted">{subtitle}</span>}
            </span>
            <InviteControl person={person} state={inviteState(p.userId)} onInvite={onInvite} />
          </li>
        );
      })}
    </ul>
  );
}
