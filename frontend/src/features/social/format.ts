import { monthNames } from "@/features/profile/format";

// shortDate shows a day as "3 Oct 2026", as the design's connections list does.
export function shortDate(d: Date): string {
  return `${d.getDate()} ${monthNames[d.getMonth()].slice(0, 3)} ${d.getFullYear()}`;
}

// daysAgo counts calendar days, in local time, from then to now.
function daysAgo(then: Date, now: Date): number {
  const day = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  return Math.round((day(now) - day(then)) / 86_400_000);
}

// sentLine is the line under a sent request: "Sent 2 days ago · Pending".
export function sentLine(createdAt: string, now = new Date()): string {
  const then = new Date(createdAt);
  const days = daysAgo(then, now);
  let when: string;
  if (days <= 0) when = "today";
  else if (days === 1) when = "yesterday";
  else if (days < 7) when = `${days} days ago`;
  else when = `on ${shortDate(then)}`;
  return `Sent ${when} · Pending`;
}

// connectedLine is the line under a connection: "Connected 3 Oct 2026", or "Connected
// today" for one made today.
export function connectedLine(connectedAt: string, now = new Date()): string {
  const then = new Date(connectedAt);
  return daysAgo(then, now) <= 0 ? "Connected today" : `Connected ${shortDate(then)}`;
}
