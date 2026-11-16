// Checks run in the browser before a request, so the user sees a message at once. They
// mirror the API's rules for createProject and updateProject (docs/openapi.yaml, FR-10.1);
// the server still checks everything (NFR-8.1).

import { length } from "@/features/profile/validation";

export const MIN_TITLE_LENGTH = 3;
export const MAX_TITLE_LENGTH = 150;
export const MAX_DESCRIPTION_LENGTH = 5000;
export const MAX_ABSTRACT_LENGTH = 3000;
export const MAX_REQUIRED_SKILLS = 20;

export type ProjectField = "title" | "description" | "abstract" | "requiredSkills";

export type ProjectValues = {
  title: string;
  description: string;
  abstract: string;
  requiredSkills: string[];
};

export type ProjectErrors = Partial<Record<ProjectField, string>>;

export const projectMessages = {
  titleShort: `Enter a title of at least ${MIN_TITLE_LENGTH} characters.`,
  titleLong: `The title can be at most ${MAX_TITLE_LENGTH} characters.`,
  descriptionRequired: "Describe the project.",
  descriptionLong: `The description can be at most ${MAX_DESCRIPTION_LENGTH.toLocaleString("en-US")} characters.`,
  abstractLong: `The abstract can be at most ${MAX_ABSTRACT_LENGTH.toLocaleString("en-US")} characters.`,
  skillsTooMany: `A project can list at most ${MAX_REQUIRED_SKILLS} required skills.`,
  skillInvalid: "One of the skills is not valid. Remove it and try again.",
};

// validateProject checks the form's fields, trimmed as the server trims them.
export function validateProject(values: ProjectValues): ProjectErrors {
  const errors: ProjectErrors = {};
  const title = length(values.title.trim());
  if (title < MIN_TITLE_LENGTH) errors.title = projectMessages.titleShort;
  else if (title > MAX_TITLE_LENGTH) errors.title = projectMessages.titleLong;
  const description = length(values.description.trim());
  if (description === 0) errors.description = projectMessages.descriptionRequired;
  else if (description > MAX_DESCRIPTION_LENGTH) errors.description = projectMessages.descriptionLong;
  if (length(values.abstract.trim()) > MAX_ABSTRACT_LENGTH) errors.abstract = projectMessages.abstractLong;
  if (values.requiredSkills.length > MAX_REQUIRED_SKILLS) errors.requiredSkills = projectMessages.skillsTooMany;
  return errors;
}

// serverFieldError places a bad_request about one field under that field: "requiredSkills[2]"
// is the skills list. It returns null for a field the form does not have.
export function serverFieldError(field: string | undefined): ProjectErrors | null {
  switch (field) {
    case "title":
      return { title: projectMessages.titleShort.replace(/\.$/, "") + ` and at most ${MAX_TITLE_LENGTH}.` };
    case "description":
      return { description: `Enter a description of 1 to ${MAX_DESCRIPTION_LENGTH.toLocaleString("en-US")} characters.` };
    case "abstract":
      return { abstract: projectMessages.abstractLong };
    default:
      if (field === "requiredSkills") return { requiredSkills: projectMessages.skillsTooMany };
      if (field?.startsWith("requiredSkills[")) return { requiredSkills: projectMessages.skillInvalid };
      return null;
  }
}
