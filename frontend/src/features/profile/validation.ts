// Checks run in the browser before a request, so the user sees a message at once. They
// mirror the server's rules in backend/internal/profile/usecase (validate.go, tags.go,
// education.go); the server still checks everything (NFR-8.1).

import { isCountryCode } from "./countries";

export const MAX_FULL_NAME_LENGTH = 200;
export const MAX_HEADLINE_LENGTH = 220;
export const MAX_BIO_LENGTH = 2000;
export const MAX_AFFILIATION_LENGTH = 200; // institution, department and job title
export const MAX_TAG_NAME_LENGTH = 60;
export const MAX_INTERESTS = 20;
export const MAX_SKILLS = 30;
export const MAX_EDUCATION_INSTITUTION_LENGTH = 200;
export const MAX_DEGREE_LENGTH = 100;
export const MAX_FIELD_OF_STUDY_LENGTH = 100;
export const MIN_EDUCATION_YEAR = 1950;
export const MAX_YEARS_AHEAD = 10;

// length counts characters (code points), as the server does, not UTF-16 units.
export const length = (s: string) => [...s].length;

const tooLong = (what: string, max: number) => `${what} can be at most ${max} characters.`;

export type IntroField = "fullName" | "headline" | "jobTitle" | "institution" | "department" | "country";
export type IntroValues = Record<IntroField, string>;
export type IntroErrors = Partial<Record<IntroField, string>>;

export const introMessages = {
  fullNameRequired: "Enter your full name.",
  countryInvalid: "Choose a country from the list.",
};

const introLimits: [Exclude<IntroField, "country">, string, number][] = [
  ["fullName", "Your full name", MAX_FULL_NAME_LENGTH],
  ["headline", "The headline", MAX_HEADLINE_LENGTH],
  ["jobTitle", "The job title", MAX_AFFILIATION_LENGTH],
  ["institution", "The institution", MAX_AFFILIATION_LENGTH],
  ["department", "The department", MAX_AFFILIATION_LENGTH],
];

// validateIntro checks the header card's fields (FR-4.1). country is a code or empty.
export function validateIntro(values: IntroValues): IntroErrors {
  const errors: IntroErrors = {};
  if (values.fullName.trim() === "") errors.fullName = introMessages.fullNameRequired;
  for (const [field, what, max] of introLimits) {
    if (!errors[field] && length(values[field].trim()) > max) errors[field] = tooLong(what, max);
  }
  const country = values.country.trim().toUpperCase();
  if (country !== "" && !isCountryCode(country)) errors.country = introMessages.countryInvalid;
  return errors;
}

// validateBio checks the About text (FR-4.1).
export function validateBio(bio: string): string | undefined {
  return length(bio.trim()) > MAX_BIO_LENGTH ? tooLong("The bio", MAX_BIO_LENGTH) : undefined;
}

// fieldForProfileCode says which field a PUT /profiles/me error code belongs to.
export function fieldForProfileCode(code: string): IntroField | "bio" | undefined {
  switch (code) {
    case "invalid_full_name":
      return "fullName";
    case "invalid_headline":
      return "headline";
    case "invalid_bio":
      return "bio";
    case "invalid_institution":
      return "institution";
    case "invalid_department":
      return "department";
    case "invalid_job_title":
      return "jobTitle";
    case "invalid_country":
      return "country";
  }
  return undefined;
}

// normalizeTagName trims a tag name and turns every run of whitespace into one space, as
// the server does before it compares names.
export function normalizeTagName(name: string): string {
  return name.trim().replace(/\s+/g, " ");
}

// sameTag reports whether two tag names are the same tag: equal ignoring letter case
// after normalizing (FR-4.2).
export function sameTag(a: string, b: string): boolean {
  return normalizeTagName(a).toLocaleLowerCase() === normalizeTagName(b).toLocaleLowerCase();
}

export function validateTagName(name: string): string | undefined {
  return length(normalizeTagName(name)) > MAX_TAG_NAME_LENGTH ? tooLong("A tag", MAX_TAG_NAME_LENGTH) : undefined;
}

export type EducationField = "institution" | "degree" | "fieldOfStudy" | "startYear" | "endYear";
// current means "I currently study here": the entry has no end year.
export type EducationValues = Record<EducationField, string> & { current: boolean };
export type EducationErrors = Partial<Record<EducationField, string>>;

export const educationMessages = {
  institutionRequired: "Enter the institution.",
  startYearRequired: "Enter the start year.",
  endYearRequired: "Enter the end year, or tick “I currently study here”.",
  endBeforeStart: "The end year cannot be before the start year.",
};

// latestYear is the latest start or end year the server accepts.
export function latestYear(now = new Date()): number {
  return now.getFullYear() + MAX_YEARS_AHEAD;
}

function year(value: string): number | undefined {
  return /^\d{4}$/.test(value.trim()) ? Number(value.trim()) : undefined;
}

// validateEducation checks an education entry (FR-4.4).
export function validateEducation(values: EducationValues, now = new Date()): EducationErrors {
  const errors: EducationErrors = {};
  const latest = latestYear(now);
  const yearRange = `Enter a year from ${MIN_EDUCATION_YEAR} to ${latest}.`;
  const institution = values.institution.trim();
  if (institution === "") errors.institution = educationMessages.institutionRequired;
  else if (length(institution) > MAX_EDUCATION_INSTITUTION_LENGTH)
    errors.institution = tooLong("The institution", MAX_EDUCATION_INSTITUTION_LENGTH);
  if (length(values.degree.trim()) > MAX_DEGREE_LENGTH) errors.degree = tooLong("The degree", MAX_DEGREE_LENGTH);
  if (length(values.fieldOfStudy.trim()) > MAX_FIELD_OF_STUDY_LENGTH)
    errors.fieldOfStudy = tooLong("The field of study", MAX_FIELD_OF_STUDY_LENGTH);

  const start = year(values.startYear);
  if (values.startYear.trim() === "") errors.startYear = educationMessages.startYearRequired;
  else if (start === undefined || start < MIN_EDUCATION_YEAR || start > latest) errors.startYear = yearRange;

  if (!values.current) {
    const end = year(values.endYear);
    if (values.endYear.trim() === "") errors.endYear = educationMessages.endYearRequired;
    else if (end === undefined || end < MIN_EDUCATION_YEAR || end > latest) errors.endYear = yearRange;
    else if (start !== undefined && end < start) errors.endYear = educationMessages.endBeforeStart;
  }
  return errors;
}

// fieldForEducationCode says which field an education error code belongs to.
export function fieldForEducationCode(code: string): EducationField | undefined {
  switch (code) {
    case "invalid_institution":
      return "institution";
    case "invalid_degree":
      return "degree";
    case "invalid_field_of_study":
      return "fieldOfStudy";
    case "invalid_start_year":
      return "startYear";
    case "invalid_end_year":
      return "endYear";
  }
  return undefined;
}

// genericError is shown for any failure that belongs to no field. The server's text is
// not shown for these, as on the profile page.
export const genericError = "Something went wrong and nothing was saved. Please try again.";
