import { countryOptions } from "./countries";
import {
  educationMessages,
  type EducationValues,
  experienceMessages,
  type ExperienceValues,
  fieldForExperienceCode,
  introMessages,
  type IntroValues,
  normalizeTagName,
  sameTag,
  validateBio,
  validateEducation,
  validateExperience,
  validateIntro,
} from "./validation";

const intro: IntroValues = {
  fullName: "Ada Lovelace",
  headline: "",
  jobTitle: "",
  institution: "",
  department: "",
  country: "GB",
  city: "",
};

describe("validateIntro", () => {
  it("accepts a name and a known country", () => {
    expect(validateIntro(intro)).toEqual({});
    expect(validateIntro({ ...intro, country: "" })).toEqual({});
    expect(validateIntro({ ...intro, country: "de" })).toEqual({});
  });

  it.each(["", "   "])("refuses the empty name %j", (fullName) => {
    expect(validateIntro({ ...intro, fullName })).toEqual({
      fullName: introMessages.fullNameRequired,
    });
  });

  it.each(["ZZ", "XK", "GBR", "1"])("refuses the country %j", (country) => {
    expect(validateIntro({ ...intro, country })).toEqual({
      country: introMessages.countryInvalid,
    });
  });

  it("counts characters, not UTF-16 units, against the limits", () => {
    expect(validateIntro({ ...intro, fullName: "😀".repeat(200) })).toEqual({});
    expect(
      validateIntro({ ...intro, fullName: "a".repeat(201) }).fullName,
    ).toMatch(/at most 200/);
    expect(
      validateIntro({ ...intro, headline: "a".repeat(221) }).headline,
    ).toMatch(/at most 220/);
    expect(
      validateIntro({ ...intro, department: "a".repeat(201) }).department,
    ).toMatch(/at most 200/);
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
    expect(normalizeTagName("  Machine \t  Learning\n")).toBe(
      "Machine Learning",
    );
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
    grade: "",
    description: "",
    current: false,
  };

  it("accepts a finished and a current entry", () => {
    expect(validateEducation(entry, now)).toEqual({});
    expect(
      validateEducation({ ...entry, endYear: "", current: true }, now),
    ).toEqual({});
  });

  it("needs an institution and a start year", () => {
    const errors = validateEducation(
      { ...entry, institution: " ", startYear: "" },
      now,
    );
    expect(errors.institution).toBe(educationMessages.institutionRequired);
    expect(errors.startYear).toBe(educationMessages.startYearRequired);
  });

  it("keeps years from 1950 to ten years ahead", () => {
    expect(
      validateEducation({ ...entry, startYear: "1949" }, now).startYear,
    ).toMatch(/1950 to 2036/);
    expect(
      validateEducation({ ...entry, endYear: "2037" }, now).endYear,
    ).toMatch(/1950 to 2036/);
    expect(
      validateEducation({ ...entry, startYear: "20x2" }, now).startYear,
    ).toMatch(/1950 to 2036/);
  });

  it("refuses an end year before the start year, and a missing one unless current", () => {
    expect(validateEducation({ ...entry, endYear: "2021" }, now).endYear).toBe(
      educationMessages.endBeforeStart,
    );
    expect(validateEducation({ ...entry, endYear: "" }, now).endYear).toBe(
      educationMessages.endYearRequired,
    );
  });
});

describe("validateExperience", () => {
  const now = new Date("2026-10-15T12:00:00Z");
  const valid: ExperienceValues = {
    title: "Research Assistant",
    employmentType: "research_assistant",
    organization: "University of Dhaka",
    location: "",
    locationType: "",
    description: "",
    startMonth: "3",
    startYear: "2025",
    endMonth: "",
    endYear: "",
    current: true,
  };

  it("accepts a current role without an end date", () => {
    expect(validateExperience(valid, now)).toEqual({});
  });

  it("requires the title, employment type, organization and start date", () => {
    const errors = validateExperience(
      {
        ...valid,
        title: "  ",
        employmentType: "",
        organization: "",
        startMonth: "",
        startYear: "2025",
      },
      now,
    );
    expect(errors).toEqual({
      title: experienceMessages.titleRequired,
      employmentType: experienceMessages.employmentTypeRequired,
      organization: experienceMessages.organizationRequired,
      startDate: experienceMessages.startDateRequired,
    });
  });

  it("requires an end date unless the role is current", () => {
    expect(validateExperience({ ...valid, current: false }, now).endDate).toBe(
      experienceMessages.endDateRequired,
    );
    expect(
      validateExperience({ ...valid, current: false, endMonth: "4" }, now)
        .endDate,
    ).toBe(experienceMessages.endDateRequired);
  });

  it("refuses an end before the start, and allows the same month", () => {
    const past = { ...valid, current: false, endYear: "2025" };
    expect(validateExperience({ ...past, endMonth: "2" }, now).endDate).toBe(
      experienceMessages.endBeforeStart,
    );
    expect(validateExperience({ ...past, endMonth: "3" }, now)).toEqual({});
  });

  it("refuses months after this one in UTC", () => {
    expect(
      validateExperience({ ...valid, startMonth: "11", startYear: "2026" }, now)
        .startDate,
    ).toBe(experienceMessages.startInFuture);
    expect(
      validateExperience(
        { ...valid, startMonth: "10", startYear: "2026" },
        now,
      ),
    ).toEqual({});
    const ended = { ...valid, current: false, endMonth: "11", endYear: "2026" };
    expect(validateExperience(ended, now).endDate).toBe(
      experienceMessages.endInFuture,
    );
  });

  it("checks lengths after trimming, and one line for title, organization and location", () => {
    const errors = validateExperience(
      {
        ...valid,
        title: "a".repeat(101),
        organization: "b".repeat(151),
        location: "c".repeat(101),
        description: "d".repeat(2001),
      },
      now,
    );
    expect(errors).toEqual({
      title: "The title can be at most 100 characters.",
      organization: "The organization can be at most 150 characters.",
      location: "The location can be at most 100 characters.",
      description: "The description can be at most 2000 characters.",
    });
    expect(
      validateExperience({ ...valid, title: ` ${"a".repeat(100)} ` }, now),
    ).toEqual({});
    expect(
      validateExperience({ ...valid, location: "Dhaka\nBangladesh" }, now)
        .location,
    ).toBe("The location must be one line.");
    expect(
      validateExperience({ ...valid, description: "One\nTwo" }, now),
    ).toEqual({});
  });

  it("maps the server's codes to fields", () => {
    expect(fieldForExperienceCode("invalid_title")).toBe("title");
    expect(fieldForExperienceCode("invalid_employment_type")).toBe(
      "employmentType",
    );
    expect(fieldForExperienceCode("invalid_location_type")).toBe(
      "locationType",
    );
    expect(fieldForExperienceCode("invalid_start_date")).toBe("startDate");
    for (const code of [
      "invalid_end_date",
      "end_date_required",
      "end_date_not_allowed",
      "invalid_date_range",
    ]) {
      expect(fieldForExperienceCode(code)).toBe("endDate");
    }
    expect(fieldForExperienceCode("invalid_description")).toBe("description");
    expect(fieldForExperienceCode("experience_limit_reached")).toBeUndefined();
  });
});
