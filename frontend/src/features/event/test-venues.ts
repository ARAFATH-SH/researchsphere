import type { ResearchField, Venue, VenuePage, VenueSummary } from "./api";

export const fields: ResearchField[] = [
  { key: "bangla_nlp", label: "Natural language processing" },
  { key: "explainable_ai", label: "Artificial intelligence and machine learning" },
  { key: "health_informatics", label: "Health informatics" },
  { key: "other", label: "Other" },
];

// now is the clock of every test: 1 Feb 2027, 00:00 UTC.
export const now = new Date("2027-02-01T00:00:00Z");

export function summary(over: Partial<VenueSummary> = {}): VenueSummary {
  return {
    id: "0192f0a0-0000-7000-8000-000000000001",
    name: "International Conference on Digital Health",
    shortName: "ICDH",
    type: "conference",
    fields: [fields[2]],
    attendance: "hybrid",
    city: "Singapore",
    country: "SG",
    startsOn: "2027-03-12",
    endsOn: "2027-03-14",
    timeZone: "AoE",
    // 15 Feb 2027 23:59:59 AoE: 14 days and 12 hours after now.
    submissionDeadline: { local: "2027-02-15T23:59:59", at: "2027-02-16T11:59:59Z" },
    abstractDeadline: null,
    notificationAt: null,
    officialUrl: "https://icdh.example.org/cfp",
    linkKind: "official",
    rolling: false,
    past: false,
    source: "manual",
    ...over,
  };
}

export function venue(over: Partial<Venue> = {}): Venue {
  return {
    ...summary(),
    scope: "Digital tools for health.\n\nTopics include <b>sensing</b>.",
    createdAt: "2026-11-01T00:00:00Z",
    updatedAt: "2026-11-01T00:00:00Z",
    ...over,
  };
}

export const journal = summary({
  id: "0192f0a0-0000-7000-8000-000000000009",
  name: "Scientific Reports",
  shortName: null,
  type: "journal",
  attendance: null,
  city: null,
  country: null,
  startsOn: null,
  endsOn: null,
  timeZone: null,
  submissionDeadline: null,
  rolling: true,
  source: "openalex",
  linkKind: "issn_portal",
  officialUrl: "https://portal.issn.org/resource/ISSN/2045-2322",
});

export function venuePage(items: VenueSummary[] = [summary()], over: Partial<VenuePage> = {}): VenuePage {
  return {
    items,
    page: 1,
    pageSize: 20,
    total: items.length,
    facets: {
      types: [
        { type: "conference", count: 12 },
        { type: "journal", count: 3 },
        { type: "workshop", count: 4 },
        { type: "special_issue", count: 0 },
      ],
      fields: fields.map((f, i) => ({ ...f, count: i + 1 })),
    },
    ...over,
  };
}
