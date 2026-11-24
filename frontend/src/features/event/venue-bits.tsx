import { chipClass } from "@/components/ui/chip";

import type { ResearchField, VenueSummary, VenueTime, VenueType } from "./api";
import { typeBadgeClass, typeLabels } from "./labels";
import { countdown, countdownClass, deadlineLabel } from "./time";

// TypeBadge is the venue's type as a coloured chip.
export function TypeBadge({ type }: { type: VenueType }) {
  return <span className={`${chipClass} ${typeBadgeClass[type]}`}>{typeLabels[type]}</span>;
}

// FieldChips lists the venue's research fields.
export function FieldChips({ fields }: { fields: ResearchField[] }) {
  return (
    <ul aria-label="Research fields" className="flex flex-wrap gap-1.5">
      {fields.map((f) => (
        <li key={f.key} className={`${chipClass} border border-rs-border bg-rs-surface font-normal text-rs-text`}>
          {f.label}
        </li>
      ))}
    </ul>
  );
}

// DeadlineText is one deadline in the venue's zone and the reader's, with the countdown when
// withCountdown is set. The countdown is words ("in 12 days"), so colour is never the only sign.
export function DeadlineText({
  time,
  zone,
  userZone,
  now,
  withCountdown = true,
}: {
  time: VenueTime;
  zone: string;
  userZone: string;
  now: Date;
  withCountdown?: boolean;
}) {
  const left = countdown(time.at, now);
  return (
    <>
      <time dateTime={time.at}>{deadlineLabel(time, zone, userZone)}</time>
      {withCountdown && (
        <>
          {" · "}
          <span data-tone={left.tone} className={countdownClass[left.tone]}>
            {left.text}
          </span>
        </>
      )}
    </>
  );
}

// deadlineTone is the colour of a venue's deadline line.
export function deadlineTone(v: Pick<VenueSummary, "submissionDeadline">, now: Date): string {
  return v.submissionDeadline ? countdownClass[countdown(v.submissionDeadline.at, now).tone] : "text-rs-heading";
}

// OfficialLink opens the venue's site in a new tab. An ISSN portal record says so.
export function OfficialLink({ venue, className = "" }: { venue: Pick<VenueSummary, "officialUrl" | "linkKind" | "name">; className?: string }) {
  const label = venue.linkKind === "issn_portal" ? "ISSN portal record" : "Call for papers";
  return (
    <a
      href={venue.officialUrl}
      target="_blank"
      rel="noopener noreferrer"
      className={`text-rs-accent-strong underline-offset-2 hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none ${className}`.trim()}
    >
      {label}
      <span aria-hidden="true"> ↗</span>
      <span className="sr-only"> of {venue.name} (opens in a new tab)</span>
    </a>
  );
}
