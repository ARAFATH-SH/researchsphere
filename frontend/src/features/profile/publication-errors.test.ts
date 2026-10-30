import {
  emptyManual,
  manualBody,
  publicationError,
  retryMinutes,
  validateManual,
} from "./publication-errors";
import { failure } from "./test-publications";
import { genericError } from "./validation";

describe("retryMinutes", () => {
  it.each([
    [undefined, 1],
    [0, 1],
    [30, 1],
    [60, 1],
    [61, 2],
    [3599, 60],
  ])("%s seconds is %s minutes", (seconds, want) => {
    expect(retryMinutes(seconds)).toBe(want);
  });
});

describe("publicationError", () => {
  it.each([
    ["invalid_doi", "doi"],
    ["doi_not_found", "doi"],
    ["publication_already_added", "doi"],
    ["duplicate_doi", "doi"],
    ["invalid_title", "title"],
    ["invalid_year", "year"],
    ["invalid_url", "url"],
    ["invalid_orcid_id", "orcid"],
    ["orcid_taken", "orcid"],
    ["orcid_not_found", "orcid"],
    ["publication_limit_reached", "form"],
    ["publication_shared_readonly", "form"],
  ])("puts %s under %s", (code, field) => {
    expect(publicationError(failure(code, 400)).field).toBe(field);
  });

  it("says in minutes when to try again after rate_limited", () => {
    expect(publicationError(failure("rate_limited", 429, "x", 1500))).toEqual({
      field: "form",
      message: "You've made too many requests. Try again in 25 minutes.",
    });
    expect(publicationError(failure("rate_limited", 429, "x", 20)).message).toContain("1 minute.");
  });

  it("does not show the server's text for an unknown code", () => {
    expect(publicationError(failure("internal_error", 500))).toEqual({
      field: "form",
      message: genericError,
    });
  });
});

describe("validateManual", () => {
  const now = new Date("2026-10-15T12:00:00Z");
  const ok = { ...emptyManual, title: "A paper", year: "2024" };

  it("accepts the required fields", () => {
    expect(validateManual(ok, now)).toEqual({});
  });

  it("asks for the title and year", () => {
    expect(validateManual(emptyManual, now)).toEqual({
      title: "Enter the title.",
      year: "Enter the year.",
    });
  });

  it.each(["1899", "2028", "20x4", "24"])("refuses the year %s", (year) => {
    expect(validateManual({ ...ok, year }, now).year).toBe("Enter a year from 1900 to 2027.");
  });

  it("accepts next year", () => {
    expect(validateManual({ ...ok, year: "2027" }, now)).toEqual({});
  });

  it("refuses a link that is not http or https, and an abstract over 5000 characters", () => {
    const found = validateManual({ ...ok, url: "ftp://x.org", abstract: "a".repeat(5001) }, now);
    expect(Object.keys(found).sort()).toEqual(["abstract", "url"]);
  });
});

describe("manualBody", () => {
  it("trims and sends null for an empty optional field", () => {
    expect(
      manualBody({ ...emptyManual, title: "  T ", year: " 2024 ", venue: "  ", doi: " 10.1/x " }),
    ).toEqual({ title: "T", year: 2024, venue: null, url: null, doi: "10.1/x", abstract: null });
  });
});
