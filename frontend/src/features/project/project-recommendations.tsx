"use client";

import Link from "next/link";
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";

import { rsButtonClass } from "@/components/ui/button";
import { homeCard } from "@/features/home/cards";
import type { MissingField } from "@/features/recommendation/api";
import { AITag, MatchBadge, NeedsProfileCard, Shared } from "@/features/recommendation/recommendation-list";
import { processingRetryMs, useRecommendations } from "@/features/recommendation/recommendations-provider";

import { getProjectRecommendations, type ProjectRecommendation } from "./api";

export const allProjectRecommendationsHref = "/projects/recommended";

export type ProjectRecommendationsState =
  | { status: "loading" }
  | { status: "error" }
  | { status: "needs_profile"; missing: MissingField[] }
  | { status: "processing" }
  | { status: "ready"; rows: ProjectRecommendation[] };

type ProjectRecommendations = { state: ProjectRecommendationsState; retry: () => void };

const Context = createContext<ProjectRecommendations | null>(null);

// ProjectRecommendationsProvider reads the caller's project recommendations (AI-3.1) once
// for the page it wraps. While the answer is "processing" it asks once more after a minute.
export function ProjectRecommendationsProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<ProjectRecommendationsState>({ status: "loading" });
  const generation = useRef(0);
  const started = useRef(false);
  const retried = useRef(false);

  const load = useCallback(async (showLoading: boolean) => {
    const mine = ++generation.current;
    if (showLoading) setState({ status: "loading" });
    const res = await getProjectRecommendations();
    if (mine !== generation.current) return;
    if (!res.ok) setState({ status: "error" });
    else if (res.data.state === "needs_profile") setState({ status: "needs_profile", missing: res.data.missing });
    else if (res.data.state === "processing") setState({ status: "processing" });
    else setState({ status: "ready", rows: res.data.items });
  }, []);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    void load(false);
  }, [load]);

  useEffect(() => {
    if (state.status !== "processing" || retried.current) return;
    const timer = setTimeout(() => {
      retried.current = true;
      void load(false);
    }, processingRetryMs);
    return () => clearTimeout(timer);
  }, [state.status, load]);

  const retry = useCallback(() => void load(true), [load]);
  const value = useMemo(() => ({ state, retry }), [state, retry]);
  return <Context.Provider value={value}>{children}</Context.Provider>;
}

const heading = "text-rs-lead leading-rs-snug font-medium text-rs-text";

const infoText =
  "How closely each project matches your research profile: your interests, skills, bio and publications against the project’s title, description and skills. Not based on popularity.";

// ProjectRecommendationList shows the caller's project recommendations: the narrow
// "Projects for you" card of design/screens/Home.html with the first limit rows (home), or
// every row at /projects/recommended (page). On Home a profile with nothing to match on is
// left to the collaborators card above, which asks for it for both lists.
export function ProjectRecommendationList({ limit, variant }: { limit: number; variant: "home" | "page" }) {
  const recs = useContext(Context);
  const people = useRecommendations();
  if (!recs) return null;
  const { state } = recs;
  const page = variant === "page";

  if (state.status === "needs_profile") {
    if (!page && people?.state.status === "needs_profile") return null;
    return <NeedsProfileCard missing={state.missing} level={2} what="projects" />;
  }

  let body: ReactNode;
  switch (state.status) {
    case "loading":
      body = <p className="text-rs-body text-rs-text-muted">Loading projects…</p>;
      break;
    case "processing":
      body = (
        <p role="status" className="text-rs-body text-rs-text">
          We’re finding projects for you. Check back in a minute.
        </p>
      );
      break;
    case "error":
      body = (
        <div className="flex flex-col items-start gap-2">
          <p role="alert" className="text-rs-body text-rs-text">
            Couldn’t load project recommendations.
          </p>
          <button type="button" onClick={recs.retry} className={rsButtonClass({ variant: "outline", size: "xs" })}>
            Try again
          </button>
        </div>
      );
      break;
    case "ready": {
      const rows = page ? state.rows : state.rows.slice(0, limit);
      body =
        rows.length === 0 ? (
          <p className="text-rs-body text-rs-text-muted">No open projects match your profile right now.</p>
        ) : (
          <ul aria-label="Recommended projects" className="flex flex-col">
            {rows.map((item) => (
              <li key={item.project.id} className="border-t border-rs-divider first:border-t-0">
                <Row item={item} wide={page} />
              </li>
            ))}
          </ul>
        );
    }
  }

  const total = state.status === "ready" ? state.rows.length : 0;
  return (
    <Frame page={page} busy={state.status === "loading"}>
      {body}
      {!page && total > limit && (
        <Link
          href={allProjectRecommendationsHref}
          className="self-start text-rs-body font-medium text-rs-accent-strong hover:underline"
        >
          See all {total}
        </Link>
      )}
    </Frame>
  );
}

// Frame is the card around the list: the title with the AI tag and the score's info button.
function Frame({ page, busy, children }: { page: boolean; busy: boolean; children: ReactNode }) {
  const headingId = useId();
  const infoId = useId();
  const [infoOpen, setInfoOpen] = useState(false);
  const H = page ? "h1" : "h2";
  return (
    <section aria-labelledby={headingId} aria-busy={busy} className={`${homeCard} flex flex-col gap-3 p-4`}>
      <div className="flex flex-col gap-2">
        <div className="flex items-center gap-1.5">
          <H id={headingId} className={page ? "text-rs-h3 leading-rs-tight font-bold text-rs-text" : heading}>
            Projects for you
          </H>
          <AITag />
          <button
            type="button"
            aria-label="About the match score"
            aria-expanded={infoOpen}
            aria-controls={infoId}
            onClick={() => setInfoOpen((open) => !open)}
            className="ml-auto flex size-7 shrink-0 items-center justify-center rounded-full text-rs-text-muted hover:bg-rs-subtle hover:text-rs-text focus-visible:shadow-focus focus-visible:outline-none"
          >
            <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" className="size-4">
              <circle cx="12" cy="12" r="9" />
              <path d="M12 11v5M12 8h.01" />
            </svg>
          </button>
        </div>
        <p id={infoId} hidden={!infoOpen} className="text-rs-caption text-rs-text">
          {infoText}
        </p>
        {page && (
          <p className="text-rs-caption text-rs-text-muted">
            Open projects only. Excludes your own projects and those you asked to join. Ranked only by research
            similarity — never by member counts.
          </p>
        )}
      </div>
      {children}
    </section>
  );
}

function membersText(n: number): string {
  return n === 1 ? "1 member" : `${n} members`;
}

// Row is one recommended project; the whole row links to its page (Home.html).
function Row({ item, wide }: { item: ProjectRecommendation; wide: boolean }) {
  const { project } = item;
  return (
    <Link
      href={`/projects/${project.id}`}
      className="flex flex-col items-start gap-1 py-2.5 text-rs-text no-underline hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none"
    >
      <strong className={wide ? "text-rs-lead font-bold" : "text-rs-body font-bold"}>{project.title}</strong>
      <span className="text-rs-caption text-rs-text-muted">
        Owner: {project.owner.fullName} · {membersText(project.memberCount)}
      </span>
      {wide && <span className="line-clamp-2 text-rs-body">{project.description}</span>}
      <MatchBadge score={item.score} bar={wide} />
      <Shared items={item.shared} label="Shared skills" />
    </Link>
  );
}
