"use client";

import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";

import type { Link } from "@/features/social/connect-button";
import type { Result } from "@/lib/api/call";

import {
  getPeopleRecommendations,
  type MissingField,
  maxRecommendations,
  type PeopleRecommendations,
  type PersonRecommendation,
} from "./api";

// processingRetryMs is how long a page waits before asking once more while the caller's
// profile embedding is being made (AI-1.4 makes it within a minute).
export const processingRetryMs = 60_000;

export type RecommendationsState =
  | { status: "loading" }
  | { status: "error" }
  | { status: "needs_profile"; missing: MissingField[] }
  | { status: "processing" }
  | { status: "ready"; rows: PersonRecommendation[] };

type Recommendations = {
  state: RecommendationsState;
  // links holds the connection changes made on this page, by user ID; they win over the
  // status the list was fetched with.
  links: Readonly<Record<string, Link>>;
  // onLinkChange hears a row's ConnectButton.
  onLinkChange: (userId: string, link: Link) => void;
  // retry asks again after an error.
  retry: () => void;
};

const RecommendationsContext = createContext<Recommendations | null>(null);

// RecommendationsProvider fetches the caller's recommendations once for the page it wraps,
// so every card on the page shares one request. While the answer is "processing" it asks once
// more after a minute, quietly. A row the user connects with stays where it is, showing its
// new state, until the page is left; the list is then fetched again (the API made it again
// at once) and fills the other places, without showing anyone twice. load reads the list:
// the caller's recommendations by default, or another list of researchers such as a
// project's (AI-2.3); it is read once, when the provider mounts.
export function RecommendationsProvider({
  children,
  load: loadList = getPeopleRecommendations,
}: {
  children: ReactNode;
  load?: () => Promise<Result<PeopleRecommendations>>;
}) {
  const loadListRef = useRef(loadList);
  const [state, setState] = useState<RecommendationsState>({ status: "loading" });
  const [links, setLinks] = useState<Record<string, Link>>({});
  // kept are the users whose row stays in place after a connection change.
  const kept = useRef(new Set<string>());
  // generation grows with every request; only the latest one may write its answer.
  const generation = useRef(0);
  const started = useRef(false);
  const retried = useRef(false);

  // load fetches the list. "show" replaces what the page shows, "loading" first shows the
  // loading state (a retry after an error) and "refetch" merges a ready answer into the rows
  // and otherwise keeps them.
  const load = useCallback(async (mode: "show" | "loading" | "refetch") => {
    const mine = ++generation.current;
    if (mode === "loading") setState({ status: "loading" });
    const res = await loadListRef.current();
    if (mine !== generation.current) return;
    if (mode === "refetch") {
      if (!res.ok || res.data.state !== "ready") return;
      const fresh = res.data.items;
      setState((prev) =>
        prev.status === "ready" ? { status: "ready", rows: merge(prev.rows, kept.current, fresh) } : prev,
      );
      return;
    }
    if (!res.ok) {
      setState({ status: "error" });
      return;
    }
    const data = res.data;
    if (data.state === "needs_profile") setState({ status: "needs_profile", missing: data.missing });
    else if (data.state === "processing") setState({ status: "processing" });
    else setState({ status: "ready", rows: data.items });
  }, []);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    void load("show");
  }, [load]);

  useEffect(() => {
    if (state.status !== "processing" || retried.current) return;
    const timer = setTimeout(() => {
      retried.current = true;
      void load("show");
    }, processingRetryMs);
    return () => clearTimeout(timer);
  }, [state.status, load]);

  const onLinkChange = useCallback(
    (userId: string, link: Link) => {
      setLinks((prev) => ({ ...prev, [userId]: link }));
      // The optimistic change comes first with no request ID; refetch once the server has it.
      const settled = link.status === "connected" || (link.status !== "none" && link.requestId !== null);
      if (!settled) return;
      kept.current.add(userId);
      void load("refetch");
    },
    [load],
  );

  const retry = useCallback(() => void load("loading"), [load]);

  const value = useMemo(() => ({ state, links, onLinkChange, retry }), [state, links, onLinkChange, retry]);
  return <RecommendationsContext.Provider value={value}>{children}</RecommendationsContext.Provider>;
}

// useRecommendations reads the page's recommendations; null outside a RecommendationsProvider.
export function useRecommendations(): Recommendations | null {
  return useContext(RecommendationsContext);
}

// merge keeps each kept row in its place and fills the other places, in order, with the
// fresh rows that are not shown already; fresh rows left over go at the end.
function merge(
  rows: PersonRecommendation[],
  kept: ReadonlySet<string>,
  fresh: PersonRecommendation[],
): PersonRecommendation[] {
  const queue = fresh.filter((r) => !kept.has(r.userId));
  const out: PersonRecommendation[] = [];
  for (const row of rows) {
    if (kept.has(row.userId)) out.push(row);
    else {
      const next = queue.shift();
      if (next) out.push(next);
    }
  }
  out.push(...queue);
  return out.slice(0, maxRecommendations);
}
