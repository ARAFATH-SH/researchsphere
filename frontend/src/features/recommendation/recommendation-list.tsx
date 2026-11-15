"use client";

import Link from "next/link";
import { type ReactNode, useId, useState } from "react";

import { Avatar } from "@/components/avatar";
import { rsButtonClass } from "@/components/ui/button";
import { emptyCard, homeCard } from "@/features/home/cards";
import { editHref } from "@/features/profile/edit-button";
import { ConnectButton } from "@/features/social/connect-button";

import type { MissingField, PersonRecommendation } from "./api";
import { useRecommendations } from "./recommendations-provider";

// The places a list is shown. home and profile are the narrow right-column cards of
// design/screens/Home.html and Profile.html; network is the collaborators card of
// Network.html and page the full list at /network/recommendations, both with wide rows.
export type RecommendationVariant = "home" | "profile" | "network" | "page";

type VariantConfig = {
  title: string;
  level: 2 | 3;
  intro?: string;
  wide: boolean;
};

const excludesNote =
  "Excludes you, your connections and pending requests. Ranked only by research similarity — never by citations, papers or connection counts.";

const variants: Record<RecommendationVariant, VariantConfig> = {
  home: { title: "Recommended collaborators", level: 2, wide: false },
  profile: {
    title: "Connect with researchers",
    level: 2,
    intro: "Top 3 researchers whose profiles match yours most closely.",
    wide: false,
  },
  network: { title: "Researchers to work with", level: 3, intro: excludesNote, wide: true },
  page: { title: "Researchers to work with", level: 2, intro: excludesNote, wide: true },
};

// scoreInfoText explains the match score (AI-2.4).
export const scoreInfoText =
  "How closely your research profile matches theirs, based on your interests, skills, bio and publications. Not based on popularity.";

export const allRecommendationsHref = "/network/recommendations";

const heading = "text-rs-lead leading-rs-snug font-medium text-rs-text";

// RecommendationList is a card of collaborator recommendations (AI-2) showing the first
// limit rows of the page's shared list (RecommendationsProvider). Without a profile to match
// on it asks for one (AI-4.2), while the profile's embedding is made it says so, and an
// error offers a retry. The profile variant shows only when there is someone to show.
export function RecommendationList({ limit, variant }: { limit: number; variant: RecommendationVariant }) {
  const recs = useRecommendations();
  const config = variants[variant];
  if (!recs) return null;
  const { state } = recs;

  if (variant === "profile" && (state.status !== "ready" || state.rows.length === 0)) return null;
  if (state.status === "needs_profile") return <NeedsProfileCard missing={state.missing} level={config.level} />;

  let body: ReactNode;
  switch (state.status) {
    case "loading":
      body = <p className="text-rs-body text-rs-text-muted">Loading recommendations…</p>;
      break;
    case "processing":
      body = (
        <p role="status" className="text-rs-body text-rs-text">
          We’re preparing your recommendations. Check back in a minute.
        </p>
      );
      break;
    case "error":
      body = (
        <div className="flex flex-col items-start gap-2">
          <p role="alert" className="text-rs-body text-rs-text">
            Couldn’t load recommendations.
          </p>
          <button type="button" onClick={recs.retry} className={rsButtonClass({ variant: "outline", size: "xs" })}>
            Try again
          </button>
        </div>
      );
      break;
    case "ready": {
      const rows = state.rows.slice(0, limit);
      body =
        rows.length === 0 ? (
          <p className="text-rs-body text-rs-text-muted">No new recommendations right now.</p>
        ) : (
          <ul aria-label="Recommended researchers" className={config.wide ? "flex flex-col" : "flex flex-col gap-3"}>
            {rows.map((item) => {
              const link = recs.links[item.userId];
              return (
                <li key={item.userId} className={config.wide ? "border-b border-rs-divider last:border-b-0" : ""}>
                  <Row
                    item={item}
                    wide={config.wide}
                    status={link?.status ?? item.connectionStatus}
                    requestId={link?.requestId ?? null}
                    onLinkChange={(next) => recs.onLinkChange(item.userId, next)}
                  />
                </li>
              );
            })}
          </ul>
        );
    }
  }

  const total = state.status === "ready" ? state.rows.length : 0;
  const seeAll = variant !== "page" && total > limit;
  // Wide rows run to the card's edges with their own padding; a message is padded here.
  const pad = config.wide && !(state.status === "ready" && total > 0) ? "px-4 py-3" : "";

  return (
    <Card config={config} busy={state.status === "loading"}>
      <div className={pad}>{body}</div>
      {seeAll &&
        (config.wide ? (
          <Link
            href={allRecommendationsHref}
            className="flex min-h-11 items-center justify-center border-t border-rs-divider text-rs-body font-medium text-rs-accent-strong hover:underline focus-visible:shadow-focus focus-visible:outline-none"
          >
            See all {total} recommendations
          </Link>
        ) : (
          <Link href={allRecommendationsHref} className="self-start text-rs-body font-medium text-rs-accent-strong hover:underline">
            See all {total} recommendations
          </Link>
        ))}
    </Card>
  );
}

// Card is the card around a list: a title with the AI tag and the score's info button, the
// intro, then the list. The narrow card has 16px padding; the wide one has a divided header
// and rows that run to its edges.
function Card({ config, busy, children }: { config: VariantConfig; busy: boolean; children: ReactNode }) {
  const headingId = useId();
  const infoId = useId();
  const [infoOpen, setInfoOpen] = useState(false);
  const H = config.level === 2 ? "h2" : "h3";
  const wide = config.wide;

  const header = (
    <div className="flex items-center gap-1.5">
      <H id={headingId} className={heading}>
        {config.title}
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
        <InfoIcon />
      </button>
    </div>
  );
  const info = (
    <p id={infoId} hidden={!infoOpen} className="text-rs-caption text-rs-text">
      {scoreInfoText}
    </p>
  );
  const intro = config.intro && <p className="text-rs-caption text-rs-text-muted">{config.intro}</p>;

  if (wide) {
    return (
      <section aria-labelledby={headingId} aria-busy={busy} className={`${homeCard} flex flex-col`}>
        <div className="flex flex-col gap-2 border-b border-rs-divider px-4 py-3">
          {header}
          {info}
        </div>
        {intro && <div className="px-4 pt-2.5">{intro}</div>}
        {children}
      </section>
    );
  }
  return (
    <section aria-labelledby={headingId} aria-busy={busy} className={`${homeCard} flex flex-col gap-3 p-4`}>
      <div className="flex flex-col gap-2">
        {header}
        {info}
        {intro}
      </div>
      {children}
    </section>
  );
}

// Row is one recommended researcher: photo, name to their profile, headline, the match
// score, the interests and skills both have, and Connect.
function Row({
  item,
  wide,
  status,
  requestId,
  onLinkChange,
}: {
  item: PersonRecommendation;
  wide: boolean;
  status: PersonRecommendation["connectionStatus"];
  requestId: string | null;
  onLinkChange: Parameters<typeof ConnectButton>[0]["onChange"];
}) {
  const subtitle = item.headline ?? item.institution;
  const name = (
    <Link
      href={`/profile/${item.userId}`}
      className="text-rs-body font-bold text-rs-text no-underline hover:text-rs-accent-strong hover:underline"
    >
      {item.fullName}
    </Link>
  );
  const connect = (
    <ConnectButton
      userId={item.userId}
      name={item.fullName}
      status={status}
      requestId={requestId}
      size="row"
      onChange={onLinkChange}
    />
  );

  if (wide) {
    return (
      <div className="flex flex-wrap items-center gap-3 px-4 py-3">
        <Avatar name={item.fullName} src={item.photoUrl} size="list" decorative />
        <div className="flex min-w-[180px] flex-1 flex-col gap-1">
          <p>
            {name}
            {subtitle && <span className="text-rs-caption text-rs-text-muted"> · {subtitle}</span>}
          </p>
          <MatchBadge score={item.score} bar />
          <Shared items={item.shared} />
        </div>
        <div data-testid="recommendation-connect" className="shrink-0 max-[640px]:basis-full max-[640px]:pl-[60px]">
          {connect}
        </div>
      </div>
    );
  }
  return (
    <div className="flex items-start gap-2.5">
      <Avatar name={item.fullName} src={item.photoUrl} size="post" decorative />
      <div className="flex min-w-0 flex-1 flex-col items-start gap-1">
        {name}
        {subtitle && <span className="text-rs-caption text-rs-text-muted">{subtitle}</span>}
        <MatchBadge score={item.score} />
        <Shared items={item.shared} />
        <div data-testid="recommendation-connect">{connect}</div>
      </div>
    </div>
  );
}

// MatchBadge is the score as "97% match" (AI-2.2), read as "97 percent match"; bar adds the
// score bar of the wide rows.
function MatchBadge({ score, bar = false }: { score: number; bar?: boolean }) {
  const text = (
    <>
      <span aria-hidden="true">{score}% match</span>
      <span className="sr-only">{score} percent match</span>
    </>
  );
  if (!bar) return <p className="text-rs-caption font-medium text-rs-heading">{text}</p>;
  return (
    <p className="flex items-center gap-2">
      <span aria-hidden="true" className="h-1.5 w-[120px] overflow-hidden rounded-rs-2xs bg-rs-neutral">
        <span className="block h-full bg-rs-accent-strong" style={{ width: `${score}%` }} />
      </span>
      <strong className="text-rs-caption font-bold text-rs-heading">{text}</strong>
    </p>
  );
}

// Shared lists up to 3 interests or skills both users have.
function Shared({ items }: { items: string[] }) {
  if (items.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-1">
      <span aria-hidden="true" className="text-rs-tiny text-rs-text-muted">
        Shared:
      </span>
      <ul aria-label="Shared interests and skills" className="flex flex-wrap gap-1">
        {items.map((tag) => (
          <li key={tag} className="rounded-rs-pill bg-rs-accent-soft px-2 py-0.5 text-rs-tiny text-rs-heading">
            {tag}
          </li>
        ))}
      </ul>
    </div>
  );
}

const missingLabels: Record<MissingField, string> = {
  interests: "research interests",
  skills: "skills",
  bio: "a short bio",
};

const missingAction: Record<MissingField, { label: string; href: string }> = {
  interests: { label: "Add interests", href: editHref("interests") },
  skills: { label: "Add skills", href: editHref("skills") },
  bio: { label: "Add a bio", href: editHref("about") },
};

// NeedsProfileCard is the dashed "Get better recommendations" card (AI-4.2): it names what
// the profile lacks and links to the editor of the first of them.
function NeedsProfileCard({ missing, level }: { missing: MissingField[]; level: 2 | 3 }) {
  const headingId = useId();
  const H = level === 2 ? "h2" : "h3";
  const fields = missing.length > 0 ? missing : (["interests", "skills", "bio"] as MissingField[]);
  const action = missingAction[fields[0]];
  return (
    <section aria-labelledby={headingId} className={`${emptyCard} flex flex-col gap-2 p-4`}>
      <H id={headingId} className={heading}>
        Get better recommendations
      </H>
      <p className="text-rs-body text-rs-text">
        Add {joinOr(fields.map((f) => missingLabels[f]))} and we’ll suggest collaborators — no publications needed.
      </p>
      <Link href={action.href} className={`${rsButtonClass({ size: "sm" })} self-start`}>
        {action.label}
      </Link>
    </section>
  );
}

function joinOr(parts: string[]): string {
  if (parts.length <= 1) return parts.join("");
  return `${parts.slice(0, -1).join(", ")} or ${parts[parts.length - 1]}`;
}

// AITag is the small violet "AI" tag beside a recommendation heading.
export function AITag() {
  return (
    <span className="shrink-0 rounded-rs-pill bg-rs-violet-soft px-2 py-px text-rs-tiny font-bold text-rs-violet">
      AI
    </span>
  );
}

function InfoIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" className="size-4">
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v5M12 8h.01" />
    </svg>
  );
}
