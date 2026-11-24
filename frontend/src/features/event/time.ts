import type { VenueTime } from "./api";

// The API states each deadline twice: `at`, the instant in UTC, and `local`, the wall time in
// the venue's zone (AoE, UTC or an IANA name) as the call for papers gives it. The page shows
// both: the venue's own words, then the same instant in the reader's zone.

const dayFormat = new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short", year: "numeric", timeZone: "UTC" });

// userTimeZone is the browser's IANA zone, UTC when it cannot say.
export function userTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

// zoneLabel is a short name for a zone: "AoE", "UTC", or the city of an IANA name
// ("Asia/Dhaka" is "Dhaka", "America/New_York" is "New York").
export function zoneLabel(zone: string): string {
  if (zone === "AoE" || zone === "UTC" || zone === "Etc/UTC") return zone === "AoE" ? "AoE" : "UTC";
  const city = zone.split("/").pop() ?? zone;
  return city.replace(/_/g, " ");
}

type Parts = { year: number; month: number; day: number; hour: number; minute: number };

// parseLocal reads "YYYY-MM-DDTHH:MM:SS" without any zone conversion.
function parseLocal(local: string): Parts | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}):(\d{2})(?::\d{2})?)?$/.exec(local);
  if (!m) return null;
  return { year: +m[1], month: +m[2], day: +m[3], hour: m[4] ? +m[4] : 0, minute: m[5] ? +m[5] : 0 };
}

const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const pad = (n: number) => String(n).padStart(2, "0");

function partsLabel(p: Parts, withYear: boolean): string {
  return `${p.day} ${months[p.month - 1]}${withYear ? ` ${p.year}` : ""}, ${pad(p.hour)}:${pad(p.minute)}`;
}

// instantParts is the wall time of an instant in an IANA zone.
function instantParts(at: string, zone: string): Parts {
  const f = new Intl.DateTimeFormat("en-GB", {
    timeZone: zone === "AoE" ? "Etc/GMT+12" : zone,
    year: "numeric",
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  });
  const get = (type: string) => Number(f.formatToParts(new Date(at)).find((p) => p.type === type)?.value ?? 0);
  return { year: get("year"), month: get("month"), day: get("day"), hour: get("hour"), minute: get("minute") };
}

// sameZone says whether two zone names show the same wall time for every instant we care
// about: equal names, or both UTC.
function sameZone(a: string, b: string): boolean {
  const utc = (z: string) => z === "UTC" || z === "Etc/UTC" || z === "Etc/GMT";
  return a === b || (utc(a) && utc(b));
}

// venueTimeLabel is the time as the venue states it: "15 Feb 2027, 23:59 AoE".
export function venueTimeLabel(t: VenueTime, venueZone: string): string {
  const p = parseLocal(t.local);
  return p ? `${partsLabel(p, true)} ${zoneLabel(venueZone)}` : t.local;
}

// userTimeLabel is the same instant in the reader's zone: "16 Feb, 17:59 Dhaka", with the year
// only when it differs from the venue's.
export function userTimeLabel(t: VenueTime, venueZone: string, userZone: string): string {
  const venue = parseLocal(t.local);
  const mine = instantParts(t.at, userZone);
  return `${partsLabel(mine, !venue || venue.year !== mine.year)} ${zoneLabel(userZone)}`;
}

// deadlineLabel is "15 Feb 2027, 23:59 AoE = 16 Feb, 17:59 Dhaka", or the venue's time alone
// when the reader is in the venue's zone.
export function deadlineLabel(t: VenueTime, venueZone: string, userZone: string): string {
  const own = venueTimeLabel(t, venueZone);
  if (sameZone(venueZone, userZone)) return own;
  return `${own} = ${userTimeLabel(t, venueZone, userZone)}`;
}

// tileParts is the month and day of the deadline as the venue states it, for the date tile.
export function tileParts(t: VenueTime): { month: string; day: string } {
  const p = parseLocal(t.local);
  return p ? { month: months[p.month - 1].toUpperCase(), day: String(p.day) } : { month: "", day: "" };
}

export type CountdownTone = "urgent" | "normal" | "past";
export type Countdown = { text: string; tone: CountdownTone };

const minute = 60_000;
const hour = 60 * minute;
const day = 24 * hour;
export const urgentWithin = 7 * day;

function plural(n: number, unit: string): string {
  return `${n} ${unit}${n === 1 ? "" : "s"}`;
}

// countdown says how long until the deadline: "in 12 days", "in 5 hours", "in 3 minutes", or
// once it has passed "closed 3 days ago". It is urgent under 7 days.
export function countdown(at: string, now: Date): Countdown {
  const left = new Date(at).getTime() - now.getTime();
  if (left <= 0) {
    const ago = -left;
    if (ago >= day) return { text: `closed ${plural(Math.floor(ago / day), "day")} ago`, tone: "past" };
    if (ago >= hour) return { text: `closed ${plural(Math.floor(ago / hour), "hour")} ago`, tone: "past" };
    return { text: "closed just now", tone: "past" };
  }
  const tone: CountdownTone = left < urgentWithin ? "urgent" : "normal";
  if (left >= day) return { text: `in ${plural(Math.floor(left / day), "day")}`, tone };
  if (left >= hour) return { text: `in ${plural(Math.floor(left / hour), "hour")}`, tone };
  return { text: `in ${plural(Math.max(1, Math.ceil(left / minute)), "minute")}`, tone };
}

// countdownClass is the colour of a deadline line: red under 7 days, muted once past.
export const countdownClass: Record<CountdownTone, string> = {
  urgent: "text-rs-danger",
  normal: "text-rs-heading",
  past: "text-rs-text-muted",
};

// dateRangeLabel is an event's dates: "20 Feb 2027", "12–14 Mar 2027", "30 Mar – 2 Apr 2027"
// or "30 Dec 2026 – 2 Jan 2027". Empty without a start.
export function dateRangeLabel(startsOn: string | null, endsOn: string | null): string {
  if (!startsOn) return "";
  const s = parseLocal(startsOn);
  const e = endsOn ? parseLocal(endsOn) : null;
  if (!s) return startsOn;
  if (!e || (e.year === s.year && e.month === s.month && e.day === s.day)) return dayFormat.format(Date.UTC(s.year, s.month - 1, s.day));
  if (e.year !== s.year) {
    return `${dayFormat.format(Date.UTC(s.year, s.month - 1, s.day))} – ${dayFormat.format(Date.UTC(e.year, e.month - 1, e.day))}`;
  }
  if (e.month !== s.month) return `${s.day} ${months[s.month - 1]} – ${e.day} ${months[e.month - 1]} ${e.year}`;
  return `${s.day}–${e.day} ${months[s.month - 1]} ${s.year}`;
}
