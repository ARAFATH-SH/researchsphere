// Checks run in the browser before a request, so the user sees a message at once. They
// mirror the server's rules in backend/internal/profile/usecase (validate.go, tags.go,
// education.go, experience.go, languages.go); the server still checks everything (NFR-8.1).

import { isCountryCode } from "./countries";
import { currentMonth } from "./format";

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
export const MAX_GRADE_LENGTH = 50;
export const MAX_EDUCATION_DESCRIPTION_LENGTH = 2000;
export const MAX_CITY_LENGTH = 100;
export const MIN_EDUCATION_YEAR = 1950;
export const MAX_YEARS_AHEAD = 10;

// length counts characters (code points), as the server does, not UTF-16 units.
export const length = (s: string) => [...s].length;

const tooLong = (what: string, max: number) =>
  `${what} can be at most ${max} characters.`;
const oneLine = (what: string) => `${what} must be one line.`;

// checkText checks a trimmed text field: at most max characters and, unless multiline,
// one line.
function checkText(
  value: string,
  what: string,
  max: number,
  multiline: boolean,
): string | undefined {
  const v = value.trim();
  if (length(v) > max) return tooLong(what, max);
  if (!multiline && /[\r\n]/.test(v)) return oneLine(what);
  return undefined;
}

export type IntroField =
  | "fullName"
  | "headline"
  | "jobTitle"
  | "institution"
  | "department"
  | "country"
  | "city";
export type IntroValues = Record<IntroField, string>;
export type IntroErrors = Partial<Record<IntroField, string>>;

export const introMessages = {
  fullNameRequired: "Enter your full name.",
  countryInvalid: "Choose a country from the list.",
};

const introLimits: [Exclude<IntroField, "country" | "city">, string, number][] =
  [
    ["fullName", "Your full name", MAX_FULL_NAME_LENGTH],
    ["headline", "The headline", MAX_HEADLINE_LENGTH],
    ["jobTitle", "The job title", MAX_AFFILIATION_LENGTH],
    ["institution", "The institution", MAX_AFFILIATION_LENGTH],
    ["department", "The department", MAX_AFFILIATION_LENGTH],
  ];

// validateIntro checks the header card's fields (FR-4.1). country is a code or empty.
export function validateIntro(values: IntroValues): IntroErrors {
  const errors: IntroErrors = {};
  if (values.fullName.trim() === "")
    errors.fullName = introMessages.fullNameRequired;
  for (const [field, what, max] of introLimits) {
    if (!errors[field] && length(values[field].trim()) > max)
      errors[field] = tooLong(what, max);
  }
  const country = values.country.trim().toUpperCase();
  if (country !== "" && !isCountryCode(country))
    errors.country = introMessages.countryInvalid;
  const city = checkText(values.city, "The city", MAX_CITY_LENGTH, false);
  if (city) errors.city = city;
  return errors;
}

// validateBio checks the About text (FR-4.1).
export function validateBio(bio: string): string | undefined {
  return length(bio.trim()) > MAX_BIO_LENGTH
    ? tooLong("The bio", MAX_BIO_LENGTH)
    : undefined;
}

// fieldForProfileCode says which field a PUT /profiles/me error code belongs to.
export function fieldForProfileCode(
  code: string,
): IntroField | "bio" | undefined {
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
    case "invalid_city":
      return "city";
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
  return (
    normalizeTagName(a).toLocaleLowerCase() ===
    normalizeTagName(b).toLocaleLowerCase()
  );
}

export function validateTagName(name: string): string | undefined {
  return length(normalizeTagName(name)) > MAX_TAG_NAME_LENGTH
    ? tooLong("A tag", MAX_TAG_NAME_LENGTH)
    : undefined;
}

export type EducationField =
  | "institution"
  | "degree"
  | "fieldOfStudy"
  | "startYear"
  | "endYear"
  | "grade"
  | "description";
// current means "I currently study here": the entry has no end year.
export type EducationValues = Record<EducationField, string> & {
  current: boolean;
};
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
export function validateEducation(
  values: EducationValues,
  now = new Date(),
): EducationErrors {
  const errors: EducationErrors = {};
  const latest = latestYear(now);
  const yearRange = `Enter a year from ${MIN_EDUCATION_YEAR} to ${latest}.`;
  const institution = values.institution.trim();
  if (institution === "")
    errors.institution = educationMessages.institutionRequired;
  else if (length(institution) > MAX_EDUCATION_INSTITUTION_LENGTH)
    errors.institution = tooLong(
      "The institution",
      MAX_EDUCATION_INSTITUTION_LENGTH,
    );
  if (length(values.degree.trim()) > MAX_DEGREE_LENGTH)
    errors.degree = tooLong("The degree", MAX_DEGREE_LENGTH);
  if (length(values.fieldOfStudy.trim()) > MAX_FIELD_OF_STUDY_LENGTH)
    errors.fieldOfStudy = tooLong(
      "The field of study",
      MAX_FIELD_OF_STUDY_LENGTH,
    );

  const start = year(values.startYear);
  if (values.startYear.trim() === "")
    errors.startYear = educationMessages.startYearRequired;
  else if (start === undefined || start < MIN_EDUCATION_YEAR || start > latest)
    errors.startYear = yearRange;

  if (!values.current) {
    const end = year(values.endYear);
    if (values.endYear.trim() === "")
      errors.endYear = educationMessages.endYearRequired;
    else if (end === undefined || end < MIN_EDUCATION_YEAR || end > latest)
      errors.endYear = yearRange;
    else if (start !== undefined && end < start)
      errors.endYear = educationMessages.endBeforeStart;
  }
  const grade = checkText(values.grade, "The grade", MAX_GRADE_LENGTH, false);
  if (grade) errors.grade = grade;
  const description = checkText(
    values.description,
    "The description",
    MAX_EDUCATION_DESCRIPTION_LENGTH,
    true,
  );
  if (description) errors.description = description;
  return errors;
}

// fieldForEducationCode says which field an education error code belongs to.
export function fieldForEducationCode(
  code: string,
): EducationField | undefined {
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
    case "invalid_grade":
      return "grade";
    case "invalid_description":
      return "description";
  }
  return undefined;
}

export const MAX_EXPERIENCE_ENTRIES = 30;
export const MAX_TITLE_LENGTH = 100;
export const MAX_ORGANIZATION_LENGTH = 150;
export const MAX_LOCATION_LENGTH = 100;
export const MAX_EXPERIENCE_DESCRIPTION_LENGTH = 2000;
export const MIN_EXPERIENCE_YEAR = 1950;

export type ExperienceField =
  | "title"
  | "employmentType"
  | "organization"
  | "location"
  | "locationType"
  | "startDate"
  | "endDate"
  | "description";
// The form's values as typed or chosen; a month is "1" to "12" and an empty select is "".
// current means "I am currently working in this role": the entry has no end date.
export type ExperienceValues = Record<
  | "title"
  | "employmentType"
  | "organization"
  | "location"
  | "locationType"
  | "description",
  string
> & {
  startMonth: string;
  startYear: string;
  endMonth: string;
  endYear: string;
  current: boolean;
};
export type ExperienceErrors = Partial<Record<ExperienceField, string>>;

export const experienceMessages = {
  titleRequired: "Enter the title.",
  employmentTypeRequired: "Choose an employment type.",
  organizationRequired: "Enter the organization.",
  startDateRequired: "Choose the start month and year.",
  endDateRequired:
    "Choose the end month and year, or tick “I am currently working in this role”.",
  startInFuture: "The start date cannot be in the future.",
  endInFuture: "The end date cannot be in the future.",
  endBeforeStart: "The end date cannot be before the start date.",
  limitReached: `You can add up to ${MAX_EXPERIENCE_ENTRIES} experiences.`,
};

// monthIndex turns a chosen month and year into a number that orders months, or
// undefined when either is not chosen.
function monthIndex(month: string, year: string): number | undefined {
  if (month === "" || year === "") return undefined;
  return Number(year) * 12 + Number(month);
}

// validateExperience checks a work experience entry (FR-4.8). Dates run from January
// 1950 to the current month in UTC, as on the server.
export function validateExperience(
  values: ExperienceValues,
  now = new Date(),
): ExperienceErrors {
  const errors: ExperienceErrors = {};
  const set = (field: ExperienceField, message: string | undefined) => {
    if (message) errors[field] = message;
  };
  if (values.title.trim() === "")
    errors.title = experienceMessages.titleRequired;
  else
    set("title", checkText(values.title, "The title", MAX_TITLE_LENGTH, false));
  if (values.employmentType === "")
    errors.employmentType = experienceMessages.employmentTypeRequired;
  if (values.organization.trim() === "")
    errors.organization = experienceMessages.organizationRequired;
  else
    set(
      "organization",
      checkText(
        values.organization,
        "The organization",
        MAX_ORGANIZATION_LENGTH,
        false,
      ),
    );
  set(
    "location",
    checkText(values.location, "The location", MAX_LOCATION_LENGTH, false),
  );
  set(
    "description",
    checkText(
      values.description,
      "The description",
      MAX_EXPERIENCE_DESCRIPTION_LENGTH,
      true,
    ),
  );

  const latest = currentMonth(now);
  const latestIndex = latest.year * 12 + latest.month;
  const start = monthIndex(values.startMonth, values.startYear);
  if (start === undefined)
    errors.startDate = experienceMessages.startDateRequired;
  else if (start > latestIndex)
    errors.startDate = experienceMessages.startInFuture;

  if (!values.current) {
    const end = monthIndex(values.endMonth, values.endYear);
    if (end === undefined) errors.endDate = experienceMessages.endDateRequired;
    else if (end > latestIndex) errors.endDate = experienceMessages.endInFuture;
    else if (start !== undefined && end < start)
      errors.endDate = experienceMessages.endBeforeStart;
  }
  return errors;
}

// fieldForExperienceCode says which field an experience error code belongs to.
export function fieldForExperienceCode(
  code: string,
): ExperienceField | undefined {
  switch (code) {
    case "invalid_title":
      return "title";
    case "invalid_organization":
      return "organization";
    case "invalid_employment_type":
      return "employmentType";
    case "invalid_location":
      return "location";
    case "invalid_location_type":
      return "locationType";
    case "invalid_start_date":
      return "startDate";
    case "invalid_end_date":
    case "end_date_required":
    case "end_date_not_allowed":
    case "invalid_date_range":
      return "endDate";
    case "invalid_description":
      return "description";
  }
  return undefined;
}

export const MAX_LANGUAGES = 20;
export const MAX_LANGUAGE_NAME_LENGTH = 50;

export type LanguageField = "name" | "proficiency";
export type LanguageValues = Record<LanguageField, string>;
export type LanguageErrors = Partial<Record<LanguageField, string>>;

export const languageMessages = {
  nameRequired: "Enter the language.",
  proficiencyRequired: "Choose a proficiency.",
  exists: "You've already added this language.",
  limitReached: `You can add up to ${MAX_LANGUAGES} languages.`,
};

// validateLanguage checks a language (FR-4.9). Whether the name is already on the
// profile is left to the server, which compares names ignoring letter case.
export function validateLanguage(values: LanguageValues): LanguageErrors {
  const errors: LanguageErrors = {};
  if (values.name.trim() === "") errors.name = languageMessages.nameRequired;
  else {
    const name = checkText(
      values.name,
      "The language",
      MAX_LANGUAGE_NAME_LENGTH,
      false,
    );
    if (name) errors.name = name;
  }
  if (values.proficiency === "")
    errors.proficiency = languageMessages.proficiencyRequired;
  return errors;
}

// fieldForLanguageCode says which field a language error code belongs to; a duplicate
// name belongs to the name.
export function fieldForLanguageCode(code: string): LanguageField | undefined {
  switch (code) {
    case "invalid_language_name":
    case "language_exists":
      return "name";
    case "invalid_proficiency":
      return "proficiency";
  }
  return undefined;
}

// genericError is shown for any failure that belongs to no field. The server's text is
// not shown for these, as on the profile page.
export const genericError =
  "Something went wrong and nothing was saved. Please try again.";
