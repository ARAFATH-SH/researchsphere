import { shortDate } from "@/features/social/format";

const minute = 60_000;
const hour = 60 * minute;

// daysAgo counts calendar days, in local time, from then to now.
function daysAgo(then: Date, now: Date): number {
  const day = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  return Math.round((day(now) - day(then)) / 86_400_000);
}

// relativeTime is a notification's time as the design writes it: "Just now", "5 min ago",
// "3 h ago" within 24 hours, then "Yesterday", "2 days ago" and, from 7 days, "3 Oct 2026".
export function relativeTime(iso: string, now = new Date()): string {
  const then = new Date(iso);
  const elapsed = now.getTime() - then.getTime();
  if (elapsed < minute) return "Just now";
  if (elapsed < hour) return `${Math.floor(elapsed / minute)} min ago`;
  if (elapsed < 24 * hour) return `${Math.floor(elapsed / hour)} h ago`;
  const days = daysAgo(then, now);
  if (days <= 1) return "Yesterday";
  if (days < 7) return `${days} days ago`;
  return shortDate(then);
}
