import type { ReactNode } from "react";

import type { Venue, VenueSummary } from "./api";
import { attendanceLabels, locationLabel, sourceLabels, typeLabels } from "./labels";
import { dateRangeLabel } from "./time";
import { DeadlineText, deadlineTone, OfficialLink } from "./venue-bits";

// kicker is the line above a venue's name: "Conference · Health informatics".
export function kicker(v: Pick<VenueSummary, "type" | "fields">): string {
  return [typeLabels[v.type], v.fields[0]?.label].filter(Boolean).join(" · ");
}

type DetailListProps = { venue: VenueSummary; userZone: string; now: Date; full?: boolean };

// DetailList is the venue's facts as the design's definition list: location, dates, the
// deadlines (each in the venue's zone and the reader's) and the website. full adds the abstract
// deadline, the notification date, attendance and where the entry came from.
export function DetailList({ venue: v, userZone, now, full = false }: DetailListProps) {
  const zone = v.timeZone ?? "UTC";
  const location = locationLabel(v);
  const dates = dateRangeLabel(v.startsOn, v.endsOn);
  const rows: [string, ReactNode][] = [];
  if (full && v.attendance) rows.push(["Attendance", attendanceLabels[v.attendance]]);
  if (location) rows.push(["Location", location]);
  if (dates) rows.push(["Dates", dates]);
  if (v.rolling) {
    rows.push(["Deadline", "Open submission: no deadline"]);
  } else {
    if (full && v.abstractDeadline) {
      rows.push(["Abstract", <DeadlineText key="a" time={v.abstractDeadline} zone={zone} userZone={userZone} now={now} />]);
    }
    if (v.submissionDeadline) {
      rows.push([
        "Deadline",
        <span key="d" className={`font-medium ${deadlineTone(v, now)}`}>
          <DeadlineText time={v.submissionDeadline} zone={zone} userZone={userZone} now={now} />
        </span>,
      ]);
    }
    if (full && v.notificationAt) {
      rows.push([
        "Notification",
        <DeadlineText key="n" time={v.notificationAt} zone={zone} userZone={userZone} now={now} withCountdown={false} />,
      ]);
    }
  }
  rows.push(["Website", <OfficialLink key="w" venue={v} />]);
  if (full && v.source !== "manual") rows.push(["Source", `Imported from ${sourceLabels[v.source]}`]);

  return (
    <dl className="m-0 grid grid-cols-[110px_minmax(0,1fr)] gap-x-2.5 gap-y-1.5 text-rs-body max-[400px]:grid-cols-1">
      {rows.map(([term, value]) => (
        <div key={term} className="contents">
          <dt className="text-rs-text-muted">{term}</dt>
          <dd className="m-0 min-w-0 break-words max-[400px]:mb-1">{value}</dd>
        </div>
      ))}
    </dl>
  );
}

// ScopeText shows the scope as the administrator entered it: plain text with its line breaks,
// never HTML (NFR-8.2).
export function ScopeText({ venue }: { venue: Pick<Venue, "scope"> }) {
  return <p className="m-0 text-rs-body leading-rs-relaxed whitespace-pre-wrap break-words text-rs-text">{venue.scope}</p>;
}
