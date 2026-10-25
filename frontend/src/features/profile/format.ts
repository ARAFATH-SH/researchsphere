import type {
  EmploymentType,
  Experience,
  LanguageProficiency,
  LocationType,
  Profile,
  YearMonth,
} from "./api";

// yearRange shows an education entry's years: "2022 – 2026", or "2026 – Present" while
// the entry is current.
export function yearRange(startYear: number, endYear: number | null): string {
  return `${startYear} – ${endYear ?? "Present"}`;
}

export const monthNames = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
];

// monthYear shows a month as "Mar 2025".
export function monthYear(m: YearMonth): string {
  return `${monthNames[m.month - 1].slice(0, 3)} ${m.year}`;
}

// currentMonth is this month in UTC, the latest month the server accepts for experience.
export function currentMonth(now = new Date()): YearMonth {
  return { year: now.getUTCFullYear(), month: now.getUTCMonth() + 1 };
}

const plural = (n: number, one: string, many: string) =>
  `${n} ${n === 1 ? one : many}`;

// experienceDuration counts the months from start to end, both included ("1 yr 8 mos").
// A current role (end null) runs to this month. Mar 2025 to Mar 2025 is "1 mo".
export function experienceDuration(
  start: YearMonth,
  end: YearMonth | null,
  now = new Date(),
): string {
  const last = end ?? currentMonth(now);
  const total = Math.max(
    1,
    last.year * 12 + last.month - (start.year * 12 + start.month) + 1,
  );
  const years = Math.floor(total / 12);
  const months = total % 12;
  return [
    years > 0 ? plural(years, "yr", "yrs") : "",
    months > 0 ? plural(months, "mo", "mos") : "",
  ]
    .filter(Boolean)
    .join(" ");
}

// experienceDates shows an experience entry's dates and length:
// "Mar 2025 - Present · 1 yr 8 mos".
export function experienceDates(
  e: Pick<Experience, "startDate" | "endDate">,
  now = new Date(),
): string {
  const end = e.endDate ? monthYear(e.endDate) : "Present";
  return `${monthYear(e.startDate)} - ${end} · ${experienceDuration(e.startDate, e.endDate, now)}`;
}

export const employmentTypeLabels: Record<EmploymentType, string> = {
  full_time: "Full-time",
  part_time: "Part-time",
  internship: "Internship",
  contract: "Contract",
  freelance: "Freelance",
  self_employed: "Self-employed",
  research_assistant: "Research assistant",
  teaching_assistant: "Teaching assistant",
  postdoc: "Postdoc",
  volunteer: "Volunteer",
};

export const locationTypeLabels: Record<LocationType, string> = {
  on_site: "On-site",
  hybrid: "Hybrid",
  remote: "Remote",
};

// proficiencyLabels are the language levels, lowest first (FR-4.9).
export const proficiencyLabels: Record<LanguageProficiency, string> = {
  elementary: "Elementary proficiency",
  limited_working: "Limited working proficiency",
  professional_working: "Professional working proficiency",
  full_professional: "Full professional proficiency",
  native_or_bilingual: "Native or bilingual proficiency",
};

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
export function affiliation(
  p: Pick<Profile, "jobTitle" | "institution">,
): string {
  return [p.jobTitle, p.institution].filter(Boolean).join(", ");
}

// locationLine shows where a researcher lives: "Dhaka, Bangladesh", just the country
// when no city is set, or "" when neither is.
export function locationLine(p: Pick<Profile, "city" | "country">): string {
  return [p.city, p.country && countryName(p.country)]
    .filter(Boolean)
    .join(", ");
}
