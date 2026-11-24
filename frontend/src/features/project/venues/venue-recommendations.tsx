"use client";

import Link from "next/link";
import { type ReactNode, useId, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { DeadlineText, TypeBadge } from "@/features/event/venue-bits";
import { userTimeZone } from "@/features/event/time";
import { emptyCard, homeCard } from "@/features/home/cards";

import type { RecommendedVenue } from "../api";
import { useVenueRecommendations } from "./use-venue-recommendations";

export const venuesHeading = "Where to submit";
export const venuesSubtitle = "Top 5 open venues whose scope matches your title and abstract.";
export const needsAbstractHeading = "Add an abstract to see venues";
export const needsAbstractText = "Venue suggestions compare your abstract with each venue’s scope.";
export const processingText = "Finding venues for your abstract…";
export const emptyText = "No upcoming venues match yet.";
export const journalsHeading = "Journals that fit";

type Props = {
  projectId: string;
  // abstract is the project's abstract as the page has it; a change reads the venues again.
  abstract: string | null;
  // canEditAbstract shows "Add abstract" (owner and editors, SRS 2.3); a viewer reads the text only.
  canEditAbstract: boolean;
  onAddAbstract: () => void;
  // addAbstractId is the "Add abstract" button's id, for the dialog to give the focus back.
  addAbstractId?: string;
  // frame "card" is the project page's side card; "plain" drops the border for the workspace's sheet.
  frame?: "card" | "plain";
  now?: Date;
  userZone?: string;
};

// VenueRecommendations is the "Where to submit" card of design/screens/ProjectDetail.html, for
// a project's members: the 5 open venues whose scope is most similar to the project's title and
// abstract (AI-6.1) with their score and deadline (AI-6.2), the rolling journals that fit, a
// request for an abstract when there is none (AI-6.3), and always the API's notice that a
// match says nothing about quality (AI-6.4).
export function VenueRecommendations({
  projectId,
  abstract,
  canEditAbstract,
  onAddAbstract,
  addAbstractId,
  frame = "card",
  now: fixedNow,
  userZone: fixedZone,
}: Props) {
  const { state, retry } = useVenueRecommendations(projectId, abstract ?? "");
  const [clock] = useState(() => fixedNow ?? new Date());
  const [userZone] = useState(() => fixedZone ?? userTimeZone());
  const headingId = useId();
  const plain = frame === "plain";

  if (state.status === "hidden") return null;

  if (state.status === "needs_abstract") {
    return (
      <section
        aria-labelledby={headingId}
        className={plain ? "flex flex-col gap-2" : `${emptyCard} flex flex-col gap-2 p-4`}
      >
        <h2 id={headingId} className="text-rs-lead leading-rs-snug font-medium text-rs-text">
          {needsAbstractHeading}
        </h2>
        <p className="text-rs-body text-rs-text">{needsAbstractText}</p>
        {canEditAbstract && (
          <button
            id={addAbstractId}
            type="button"
            onClick={onAddAbstract}
            className={`${rsButtonClass({ size: "sm" })} self-start`}
          >
            Add abstract
          </button>
        )}
        <Notice text={state.notice} />
      </section>
    );
  }

  let body: ReactNode;
  switch (state.status) {
    case "loading":
      body = <p className="text-rs-body text-rs-text-muted">Loading venues…</p>;
      break;
    case "processing":
      body = (
        <p role="status" className="text-rs-body text-rs-text">
          {processingText}
        </p>
      );
      break;
    case "error":
      body = (
        <div className="flex flex-col items-start gap-2">
          <p role="alert" className="text-rs-body text-rs-text">
            Couldn’t load venue suggestions.
          </p>
          <button type="button" onClick={retry} className={rsButtonClass({ variant: "outline", size: "xs" })}>
            Try again
          </button>
        </div>
      );
      break;
    case "ready":
      body = (
        <>
          {state.venues.length === 0 ? (
            <p className="border-t border-rs-divider pt-2.5 text-rs-body text-rs-text-muted">{emptyText}</p>
          ) : (
            <ul aria-label="Recommended venues" className="flex flex-col gap-2.5">
              {state.venues.map((v) => (
                <li key={v.id}>
                  <Row venue={v} now={clock} userZone={userZone} />
                </li>
              ))}
            </ul>
          )}
          {state.journals.length > 0 && <Journals journals={state.journals} now={clock} userZone={userZone} />}
        </>
      );
  }

  const notice = state.status === "loading" ? null : state.notice;
  return (
    <section
      aria-labelledby={headingId}
      aria-busy={state.status === "loading"}
      className={plain ? "flex flex-col gap-2.5" : `${homeCard} flex flex-col gap-2.5 p-4`}
    >
      <h2 id={headingId} className="text-rs-lead leading-rs-snug font-medium text-rs-text">
        {venuesHeading}
      </h2>
      <p className="text-rs-caption text-rs-text-muted">{venuesSubtitle}</p>
      {body}
      {notice && <Notice text={notice} />}
    </section>
  );
}

// Notice is the AI-6.4 sentence, shown in the open under the list.
function Notice({ text }: { text: string }) {
  return (
    <p data-testid="venue-notice" className="rounded-rs-md bg-rs-subtle px-2.5 py-2 text-rs-caption text-rs-text">
      {text}
    </p>
  );
}

function Journals({ journals, now, userZone }: { journals: RecommendedVenue[]; now: Date; userZone: string }) {
  const id = useId();
  return (
    <div className="flex flex-col gap-2.5 pt-1">
      <h3 id={id} className="text-rs-body font-medium text-rs-text">
        {journalsHeading}
      </h3>
      <ul aria-labelledby={id} className="flex flex-col gap-2.5">
        {journals.map((v) => (
          <li key={v.id}>
            <Row venue={v} now={now} userZone={userZone} />
          </li>
        ))}
      </ul>
    </div>
  );
}

// Row is one venue: the score tile at the left, the name, the type and the deadline in the
// venue's zone and the reader's with the countdown, or "Open submission" for a journal. The
// whole row opens the venue's page.
function Row({ venue: v, now, userZone }: { venue: RecommendedVenue; now: Date; userZone: string }) {
  return (
    <Link
      href={`/events/${v.id}`}
      className="flex items-center gap-2.5 rounded-control border-t border-rs-divider pt-2.5 text-rs-text no-underline hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none"
    >
      <span
        data-testid="venue-score"
        className="w-11 flex-none rounded-control bg-rs-accent-soft py-1.5 text-center text-rs-body font-bold text-rs-heading"
      >
        <span aria-hidden="true">{v.score}%</span>
        <span className="sr-only">{v.score} percent match</span>
      </span>
      <span className="flex min-w-0 flex-1 flex-col gap-1 text-rs-body">
        <strong className="font-bold break-words">
          {v.name}
          {v.shortName && <span className="font-normal text-rs-text-muted"> ({v.shortName})</span>}
        </strong>
        <span className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-rs-caption text-rs-text-muted">
          <TypeBadge type={v.type} />
          {v.submissionDeadline && v.timeZone ? (
            <span>
              Deadline{" "}
              <DeadlineText time={v.submissionDeadline} zone={v.timeZone} userZone={userZone} now={now} />
            </span>
          ) : (
            <span>Open submission</span>
          )}
        </span>
      </span>
    </Link>
  );
}
