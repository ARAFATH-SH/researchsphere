import { calendarFileName, escapeText, foldLine, venueCalendar } from "./ics";
import { journal, venue } from "./test-venues";

const stamp = new Date("2027-02-01T08:30:00Z");

describe("venueCalendar", () => {
  it("holds one event at the submission deadline in UTC", () => {
    const text = venueCalendar(venue(), stamp);
    expect(text).toBe(
      [
        "BEGIN:VCALENDAR",
        "VERSION:2.0",
        "PRODID:-//ResearchSphere//Venues//EN",
        "CALSCALE:GREGORIAN",
        "METHOD:PUBLISH",
        "BEGIN:VEVENT",
        "UID:0192f0a0-0000-7000-8000-000000000001-submission@researchsphere",
        "DTSTAMP:20270201T083000Z",
        "DTSTART:20270216T115959Z",
        "DTEND:20270216T115959Z",
        "SUMMARY:Submission deadline: ICDH: International Conference on Digital Heal",
        " th",
        "DESCRIPTION:Submission deadline: 15 Feb 2027\\, 23:59 AoE.\\nhttps://icdh.exa",
        " mple.org/cfp",
        "URL:https://icdh.example.org/cfp",
        "TRANSP:TRANSPARENT",
        "END:VEVENT",
        "END:VCALENDAR",
        "",
      ].join("\r\n"),
    );
  });

  it("adds the abstract deadline first when there is one", () => {
    const text = venueCalendar(
      venue({ abstractDeadline: { local: "2027-02-08T23:59:59", at: "2027-02-09T11:59:59Z" } }),
      stamp,
    )!;
    expect(text.match(/BEGIN:VEVENT/g)).toHaveLength(2);
    expect(text.indexOf("DTSTART:20270209T115959Z")).toBeLessThan(text.indexOf("DTSTART:20270216T115959Z"));
    expect(text).toContain("-abstract@researchsphere");
  });

  it("has nothing for a rolling journal", () => {
    expect(venueCalendar({ ...venue(), ...journal }, stamp)).toBeNull();
  });
});

describe("escapeText and foldLine", () => {
  it("escapes the characters RFC 5545 reserves", () => {
    expect(escapeText("a\\b;c,d\ne")).toBe("a\\\\b\\;c\\,d\\ne");
  });

  it("folds at 75 octets without splitting a character", () => {
    const line = "SUMMARY:" + "বাংলা".repeat(10);
    const folded = foldLine(line).split("\r\n");
    for (const part of folded) expect(Buffer.byteLength(part, "utf8")).toBeLessThanOrEqual(75);
    expect(folded.map((p, i) => (i ? p.slice(1) : p)).join("")).toBe(line);
    expect(folded.slice(1).every((p) => p.startsWith(" "))).toBe(true);
  });

  it("leaves a short line alone", () => {
    expect(foldLine("VERSION:2.0")).toBe("VERSION:2.0");
  });
});

it("names the file after the short name", () => {
  expect(calendarFileName({ name: "Long Name", shortName: "ICML 2027" })).toBe("icml-2027-deadline.ics");
  expect(calendarFileName({ name: "Journal of Ünïcode", shortName: null })).toBe("journal-of-n-code-deadline.ics");
});
