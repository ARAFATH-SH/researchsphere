import type { PubFailure } from "./publications-api";
import { genericError } from "./validation";

// PublicationField is where an error is shown: under one of the dialog's fields, or as a
// banner for the whole form.
export type PublicationField =
  | "title"
  | "year"
  | "venue"
  | "abstract"
  | "url"
  | "doi"
  | "orcid"
  | "form";

const fieldMessages: Record<string, [PublicationField, string]> = {
  invalid_title: ["title", "Enter a title of at most 500 characters."],
  invalid_year: ["year", "Enter a year from 1900 to next year."],
  invalid_venue: ["venue", "Enter a venue of at most 300 characters."],
  invalid_abstract: ["abstract", "The abstract can be at most 5000 characters."],
  invalid_url: ["url", "Enter a link that starts with http:// or https://."],
  invalid_doi: ["doi", "Enter a DOI such as 10.1145/3292500.3330701."],
  doi_not_found: [
    "doi",
    "We couldn't find a publication with this DOI. Check it, or add the publication by hand.",
  ],
  duplicate_doi: ["doi", "Another publication already has this DOI."],
  publication_already_added: ["doi", "This publication is already on your profile."],
  invalid_orcid_id: ["orcid", "Enter an ORCID iD such as 0000-0002-1825-0097."],
  orcid_taken: ["orcid", "Another profile already has this ORCID iD."],
  orcid_not_found: ["orcid", "ORCID has no record with this iD. Check the iD you saved."],
  orcid_not_set: ["orcid", "Save your ORCID iD first."],
  publication_limit_reached: [
    "form",
    "You have reached the limit of 300 publications. Remove one to add another.",
  ],
  publication_shared_readonly: [
    "form",
    "This publication can't be edited: it is shared with a co-author or was imported. You can remove it instead.",
  ],
  invalid_put_code: ["form", "Your ORCID works have changed. List your works again."],
};

// retryMinutes turns Retry-After seconds into whole minutes, rounded up, at least 1.
export function retryMinutes(seconds: number | undefined): number {
  if (seconds === undefined || seconds <= 0) return 1;
  return Math.ceil(seconds / 60);
}

// publicationError says where and how to show a refused publication request.
// metadata_unavailable is not handled here: the dialog shows the API's own message with a
// way to add the publication by hand (FR-5.6).
export function publicationError(res: PubFailure): {
  field: PublicationField;
  message: string;
} {
  if (res.code === "rate_limited") {
    const n = retryMinutes(res.retryAfter);
    return {
      field: "form",
      message: `You've made too many requests. Try again in ${n} ${n === 1 ? "minute" : "minutes"}.`,
    };
  }
  if (res.code === "network_error") return { field: "form", message: res.message };
  const known = fieldMessages[res.code];
  if (known) return { field: known[0], message: known[1] };
  return { field: "form", message: genericError };
}

// ManualValues are the text of the manual publication form.
export type ManualValues = {
  title: string;
  venue: string;
  year: string;
  url: string;
  doi: string;
  abstract: string;
};

export const emptyManual: ManualValues = {
  title: "",
  venue: "",
  year: "",
  url: "",
  doi: "",
  abstract: "",
};

export const abstractMax = 5000;

export type ManualErrors = Partial<Record<PublicationField, string>>;

// validateManual checks what the browser can check before sending; the server checks it
// all again (FR-5.1).
export function validateManual(v: ManualValues, now = new Date()): ManualErrors {
  const errors: ManualErrors = {};
  const title = v.title.trim();
  if (title === "") errors.title = "Enter the title.";
  else if ([...title].length > 500) errors.title = fieldMessages.invalid_title[1];
  if ([...v.venue.trim()].length > 300) errors.venue = fieldMessages.invalid_venue[1];
  const year = v.year.trim();
  const maxYear = now.getUTCFullYear() + 1;
  if (year === "") errors.year = "Enter the year.";
  else if (!/^\d{4}$/.test(year) || Number(year) < 1900 || Number(year) > maxYear)
    errors.year = `Enter a year from 1900 to ${maxYear}.`;
  const url = v.url.trim();
  if (url !== "" && (!/^https?:\/\/\S+$/i.test(url) || url.length > 500))
    errors.url = fieldMessages.invalid_url[1];
  if ([...v.abstract.trim()].length > abstractMax)
    errors.abstract = fieldMessages.invalid_abstract[1];
  return errors;
}

// manualBody is the request body for the form: text trimmed, an empty optional field null.
export function manualBody(v: ManualValues) {
  const opt = (s: string) => (s.trim() === "" ? null : s.trim());
  return {
    title: v.title.trim(),
    year: Number(v.year.trim()),
    venue: opt(v.venue),
    url: opt(v.url),
    doi: opt(v.doi),
    abstract: opt(v.abstract),
  };
}
