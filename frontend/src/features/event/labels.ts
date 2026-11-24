import type { VenueAttendance, VenueSource, VenueSummary, VenueType } from "./api";

export const venueTypes: VenueType[] = ["conference", "journal", "workshop", "special_issue"];

export const typeLabels: Record<VenueType, string> = {
  conference: "Conference",
  journal: "Journal",
  workshop: "Workshop",
  special_issue: "Special issue",
};

export const attendanceLabels: Record<VenueAttendance, string> = {
  in_person: "In person",
  online: "Online",
  hybrid: "Hybrid",
};

export const sourceLabels: Record<VenueSource, string> = {
  manual: "Manual",
  import: "File import",
  ccfddl: "ccf-deadlines",
  hci_deadlines: "hci-deadlines",
  openalex: "OpenAlex",
};

// syncedSources are the automatic import's: an administrator's edit locks such a venue.
export const syncedSources: VenueSource[] = ["ccfddl", "hci_deadlines", "openalex"];

export function isVenueType(value: string): value is VenueType {
  return (venueTypes as string[]).includes(value);
}

// countryName is the English name of an ISO 3166-1 alpha-2 code, or the code itself when the
// browser does not know it.
export function countryName(code: string): string {
  try {
    return new Intl.DisplayNames(["en"], { type: "region" }).of(code.toUpperCase()) ?? code;
  } catch {
    return code;
  }
}

// locationLabel is where an event takes place: "Online", "Dhaka, Bangladesh" or
// "Singapore, Singapore · hybrid". A journal or special issue has no place: "".
export function locationLabel(v: Pick<VenueSummary, "attendance" | "city" | "country">): string {
  if (v.attendance === "online") return "Online";
  const place = [v.city, v.country ? countryName(v.country) : null].filter(Boolean).join(", ");
  if (v.attendance === "hybrid") return place ? `${place} · hybrid` : "Hybrid";
  return place;
}

// typeBadgeClass colours the type badge; each type has its own soft colour.
export const typeBadgeClass: Record<VenueType, string> = {
  conference: "bg-rs-accent-soft text-rs-heading",
  workshop: "bg-rs-orange-soft text-rs-orange",
  journal: "bg-rs-violet-soft text-rs-violet",
  special_issue: "bg-rs-neutral text-rs-text",
};
