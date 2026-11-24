import type { VenueQuery, VenueType } from "./api";
import { isVenueType } from "./labels";

export const venuesPageSize = 20;
export const minQueryLength = 2;
export const maxQueryLength = 100;

// EventsTab: venues with a deadline, by nearest deadline, or journals that take submissions at
// any time (rolling), by name. The two are never mixed.
export type EventsTab = "deadlines" | "journals";

// EventsState is the whole venue list as the URL holds it.
export type EventsState = {
  tab: EventsTab;
  types: VenueType[];
  fields: string[];
  from: string;
  to: string;
  q: string;
  past: boolean;
  page: number;
};

export const emptyFilters: Omit<EventsState, "tab" | "page"> = {
  types: [],
  fields: [],
  from: "",
  to: "",
  q: "",
  past: false,
};

const isDate = (s: string) => /^\d{4}-\d{2}-\d{2}$/.test(s);

// readEvents reads the list from the URL: tab=journals, type and field (repeated), from and
// to (YYYY-MM-DD), q, past=1 and page. Values that cannot be right are dropped.
export function readEvents(params: URLSearchParams): EventsState {
  const page = Number(params.get("page") ?? "1");
  const tab: EventsTab = params.get("tab") === "journals" ? "journals" : "deadlines";
  const from = params.get("from") ?? "";
  const to = params.get("to") ?? "";
  return {
    tab,
    types: [...new Set(params.getAll("type").filter(isVenueType))],
    fields: [...new Set(params.getAll("field").filter((f) => /^[a-z0-9_]{1,64}$/.test(f)))],
    from: tab === "deadlines" && isDate(from) ? from : "",
    to: tab === "deadlines" && isDate(to) ? to : "",
    q: (params.get("q") ?? "").slice(0, maxQueryLength),
    past: tab === "deadlines" && params.get("past") === "1",
    page: Number.isInteger(page) && page >= 1 ? page : 1,
  };
}

// eventsHref is the URL of a list state; defaults are left out.
export function eventsHref(s: EventsState, base = "/events"): string {
  const params = new URLSearchParams();
  if (s.tab === "journals") params.set("tab", "journals");
  for (const t of s.types) params.append("type", t);
  for (const f of s.fields) params.append("field", f);
  if (s.tab === "deadlines") {
    if (s.from) params.set("from", s.from);
    if (s.to) params.set("to", s.to);
    if (s.past) params.set("past", "1");
  }
  if (s.q) params.set("q", s.q);
  if (s.page > 1) params.set("page", String(s.page));
  const query = params.toString();
  return query ? `${base}?${query}` : base;
}

// activeFilterCount is how many filters are set, for the phone's "Filters (n)" button.
export function activeFilterCount(s: EventsState): number {
  return s.types.length + s.fields.length + (s.from ? 1 : 0) + (s.to ? 1 : 0) + (s.past ? 1 : 0) + (s.q.trim() ? 1 : 0);
}

// rangeError says what is wrong with the deadline range, or "" when it is fine.
export function rangeError(s: Pick<EventsState, "from" | "to">): string {
  return s.from && s.to && s.to < s.from ? "The last day is before the first day." : "";
}

// toQuery is the API request of a list state. q is sent once it has 2 characters; the deadline
// range is read in the reader's zone tz.
export function toQuery(s: EventsState, tz: string, pageSize = venuesPageSize): VenueQuery {
  const q = s.q.trim();
  const journals = s.tab === "journals";
  return {
    page: s.page,
    pageSize,
    rolling: journals,
    includePast: !journals && s.past,
    types: s.types,
    fields: s.fields,
    deadlineFrom: journals ? undefined : s.from || undefined,
    deadlineTo: journals ? undefined : s.to || undefined,
    tz,
    q: q.length >= minQueryLength ? q : undefined,
  };
}
