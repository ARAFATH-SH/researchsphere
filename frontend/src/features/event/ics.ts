import type { Venue, VenueTime } from "./api";
import { venueTimeLabel } from "./time";

// An iCalendar file (RFC 5545) for a venue's deadlines, built in the browser: one event for the
// submission deadline and one for the abstract deadline when there is one. Times are in UTC
// (the "Z" form), so every calendar shows them right whatever its zone; the description keeps
// the time as the venue states it.

// escapeText escapes a TEXT value: backslash, semicolon, comma and line breaks.
export function escapeText(text: string): string {
  return text.replace(/\\/g, "\\\\").replace(/;/g, "\\;").replace(/,/g, "\\,").replace(/\r?\n/g, "\\n");
}

// utf8Length is the number of octets of one character in UTF-8.
function utf8Length(ch: string): number {
  const cp = ch.codePointAt(0) ?? 0;
  return cp < 0x80 ? 1 : cp < 0x800 ? 2 : cp < 0x10000 ? 3 : 4;
}

// foldLine splits a content line into lines of at most 75 octets, each continuation starting
// with a space, never inside a character.
export function foldLine(line: string): string {
  const out: string[] = [];
  let current = "";
  let size = 0;
  for (const ch of line) {
    const n = utf8Length(ch);
    const limit = out.length === 0 ? 75 : 74; // a continuation line starts with one space
    if (size + n > limit) {
      out.push(current);
      current = "";
      size = 0;
    }
    current += ch;
    size += n;
  }
  out.push(current);
  return out.join("\r\n ");
}

// utcStamp is an instant in the form 20270216T115959Z.
export function utcStamp(at: string | Date): string {
  return new Date(at).toISOString().replace(/[-:]/g, "").replace(/\.\d{3}/, "");
}

function event(venue: Venue, kind: "submission" | "abstract", t: VenueTime, now: Date): string[] {
  const title = venue.shortName ? `${venue.shortName}: ${venue.name}` : venue.name;
  const what = kind === "submission" ? "Submission deadline" : "Abstract deadline";
  const description = `${what}: ${venueTimeLabel(t, venue.timeZone ?? "UTC")}.\n${venue.officialUrl}`;
  return [
    "BEGIN:VEVENT",
    `UID:${venue.id}-${kind}@researchsphere`,
    `DTSTAMP:${utcStamp(now)}`,
    `DTSTART:${utcStamp(t.at)}`,
    `DTEND:${utcStamp(t.at)}`,
    `SUMMARY:${escapeText(`${what}: ${title}`)}`,
    `DESCRIPTION:${escapeText(description)}`,
    `URL:${venue.officialUrl}`,
    "TRANSP:TRANSPARENT",
    "END:VEVENT",
  ];
}

// venueCalendar is the .ics text of a venue's deadlines, or null for a rolling journal.
export function venueCalendar(venue: Venue, now: Date): string | null {
  if (!venue.submissionDeadline) return null;
  const lines = ["BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//ResearchSphere//Venues//EN", "CALSCALE:GREGORIAN", "METHOD:PUBLISH"];
  if (venue.abstractDeadline) lines.push(...event(venue, "abstract", venue.abstractDeadline, now));
  lines.push(...event(venue, "submission", venue.submissionDeadline, now));
  lines.push("END:VCALENDAR");
  return lines.map(foldLine).join("\r\n") + "\r\n";
}

// calendarFileName is a safe file name from the venue's short name or name.
export function calendarFileName(venue: Pick<Venue, "name" | "shortName">): string {
  const slug = (venue.shortName || venue.name)
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 60);
  return `${slug || "venue"}-deadline.ics`;
}

// downloadCalendar saves the venue's .ics file through a temporary link.
export function downloadCalendar(venue: Venue, now = new Date()): void {
  const text = venueCalendar(venue, now);
  if (!text) return;
  const url = URL.createObjectURL(new Blob([text], { type: "text/calendar;charset=utf-8" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = calendarFileName(venue);
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}
