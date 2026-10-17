import { countryOptions } from "./countries";
import {
  educationMessages,
  type EducationValues,
  introMessages,
  type IntroValues,
  normalizeTagName,
  sameTag,
  validateBio,
  validateEducation,
  validateIntro,
} from "./validation";

const intro: IntroValues = {
  fullName: "Ada Lovelace",
  headline: "",
  jobTitle: "",
  institution: "",
  department: "",
  country: "GB",
};

describe("validateIntro", () => {
  it("accepts a name and a known country", () => {
    expect(validateIntro(intro)).toEqual({});
    expect(validateIntro({ ...intro, country: "" })).toEqual({});
    expect(validateIntro({ ...intro, country: "de" })).toEqual({});
  });

  it.each(["", "   "])("refuses the empty name %j", (fullName) => {
    expect(validateIntro({ ...intro, fullName })).toEqual({ fullName: introMessages.fullNameRequired });
  });

  it.each(["ZZ", "XK", "GBR", "1"])("refuses the country %j", (country) => {
    expect(validateIntro({ ...intro, country })).toEqual({ country: introMessages.countryInvalid });
  });

  it("counts characters, not UTF-16 units, against the limits", () => {
    expect(validateIntro({ ...intro, fullName: "😀".repeat(200) })).toEqual({});
    expect(validateIntro({ ...intro, fullName: "a".repeat(201) }).fullName).toMatch(/at most 200/);
    expect(validateIntro({ ...intro, headline: "a".repeat(221) }).headline).toMatch(/at most 220/);
    expect(validateIntro({ ...intro, department: "a".repeat(201) }).department).toMatch(/at most 200/);
  });
});

describe("validateBio", () => {
  it("allows 2000 characters and refuses more", () => {
    expect(validateBio("a".repeat(2000))).toBeUndefined();
    expect(validateBio("a".repeat(2001))).toMatch(/at most 2000/);
  });
});

describe("countryOptions", () => {
  it("lists the 249 countries the server accepts, sorted by name", () => {
    const options = countryOptions();
    expect(options).toHaveLength(249);
    expect(options.find((o) => o.code === "DE")?.name).toBe("Germany");
    const names = options.map((o) => o.name);
    expect(names).toEqual([...names].sort((a, b) => a.localeCompare(b, "en")));
  });
});

describe("tag names", () => {
  it("normalizes whitespace and compares ignoring letter case", () => {
    expect(normalizeTagName("  Machine \t  Learning\n")).toBe("Machine Learning");
    expect(sameTag("machine  learning", "Machine Learning")).toBe(true);
    expect(sameTag("ML", "NLP")).toBe(false);
  });
});

describe("validateEducation", () => {
  const now = new Date(2026, 9, 18);
  const entry: EducationValues = {
    institution: "MIT",
    degree: "",
    fieldOfStudy: "",
    startYear: "2022",
    endYear: "2026",
    current: false,
  };

  it("accepts a finished and a current entry", () => {
    expect(validateEducation(entry, now)).toEqual({});
    expect(validateEducation({ ...entry, endYear: "", current: true }, now)).toEqual({});
  });

  it("needs an institution and a start year", () => {
    const errors = validateEducation({ ...entry, institution: " ", startYear: "" }, now);
    expect(errors.institution).toBe(educationMessages.institutionRequired);
    expect(errors.startYear).toBe(educationMessages.startYearRequired);
  });

  it("keeps years from 1950 to ten years ahead", () => {
    expect(validateEducation({ ...entry, startYear: "1949" }, now).startYear).toMatch(/1950 to 2036/);
    expect(validateEducation({ ...entry, endYear: "2037" }, now).endYear).toMatch(/1950 to 2036/);
    expect(validateEducation({ ...entry, startYear: "20x2" }, now).startYear).toMatch(/1950 to 2036/);
  });

  it("refuses an end year before the start year, and a missing one unless current", () => {
    expect(validateEducation({ ...entry, endYear: "2021" }, now).endYear).toBe(educationMessages.endBeforeStart);
    expect(validateEducation({ ...entry, endYear: "" }, now).endYear).toBe(educationMessages.endYearRequired);
  });
});
