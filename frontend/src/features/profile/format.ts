import type { Profile } from "./api";

// yearRange shows an education entry's years: "2022 – 2026", or "2026 – Present" while
// the entry is current.
export function yearRange(startYear: number, endYear: number | null): string {
  return `${startYear} – ${endYear ?? "Present"}`;
}

const regionNames = new Intl.DisplayNames(["en"], { type: "region" });

// countryName turns an ISO 3166-1 alpha-2 code into its English name ("DE" is
// "Germany"). An unknown code is shown as it is.
export function countryName(code: string): string {
  try {
    return regionNames.of(code) ?? code;
  } catch {
    return code;
  }
}

// initials lives in lib/initials.ts; the Avatar component uses it too.
export { initials } from "@/lib/initials";

// affiliation joins the job title and institution ("Researcher, University of London"),
// leaving out whichever is not set.
export function affiliation(p: Pick<Profile, "jobTitle" | "institution">): string {
  return [p.jobTitle, p.institution].filter(Boolean).join(", ");
}
