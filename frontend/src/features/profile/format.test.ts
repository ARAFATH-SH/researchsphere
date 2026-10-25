import { affiliation, countryName, experienceDates, experienceDuration, yearRange } from "./format";

// now is mid-October 2026 in UTC.
const now = new Date("2026-10-15T12:00:00Z");

describe("experienceDuration", () => {
  it("counts a role that starts and ends in the same month as one month", () => {
    expect(experienceDuration({ year: 2025, month: 3 }, { year: 2025, month: 3 }, now)).toBe("1 mo");
  });

  it("shows months only under a year, both months included", () => {
    expect(experienceDuration({ year: 2025, month: 3 }, { year: 2025, month: 10 }, now)).toBe("8 mos");
  });

  it("shows years and months", () => {
    expect(experienceDuration({ year: 2025, month: 1 }, { year: 2025, month: 12 }, now)).toBe("1 yr");
    expect(experienceDuration({ year: 2023, month: 1 }, { year: 2024, month: 12 }, now)).toBe("2 yrs");
    expect(experienceDuration({ year: 2024, month: 3 }, { year: 2025, month: 3 }, now)).toBe("1 yr 1 mo");
    expect(experienceDuration({ year: 2020, month: 6 }, { year: 2023, month: 1 }, now)).toBe("2 yrs 8 mos");
  });

  it("counts a current role up to this month", () => {
    expect(experienceDuration({ year: 2025, month: 3 }, null, now)).toBe("1 yr 8 mos");
    expect(experienceDuration({ year: 2026, month: 10 }, null, now)).toBe("1 mo");
  });

  it("uses the month in UTC", () => {
    // 2026-11-01 00:30 in UTC+6 is still October in UTC.
    expect(experienceDuration({ year: 2026, month: 10 }, null, new Date("2026-10-31T18:30:00Z"))).toBe("1 mo");
  });
});

describe("experienceDates", () => {
  it("shows the start, Present and the length of a current role", () => {
    expect(experienceDates({ startDate: { year: 2025, month: 3 }, endDate: null }, now)).toBe(
      "Mar 2025 - Present · 1 yr 8 mos",
    );
  });

  it("shows both months of a past role", () => {
    expect(experienceDates({ startDate: { year: 2023, month: 1 }, endDate: { year: 2024, month: 12 } }, now)).toBe(
      "Jan 2023 - Dec 2024 · 2 yrs",
    );
  });
});

describe("yearRange", () => {
  it("shows both years of a finished entry", () => {
    expect(yearRange(2022, 2026)).toBe("2022 – 2026");
  });

  it("shows Present while the entry is current", () => {
    expect(yearRange(2026, null)).toBe("2026 – Present");
  });
});

describe("countryName", () => {
  it("names the country of an ISO code", () => {
    expect(countryName("DE")).toBe("Germany");
    expect(countryName("GB")).toBe("United Kingdom");
  });

  it("shows a code it cannot name as it is", () => {
    expect(countryName("not a code")).toBe("not a code");
  });
});

describe("affiliation", () => {
  it("joins the job title and institution, leaving out what is not set", () => {
    expect(affiliation({ jobTitle: "Researcher", institution: "MIT" })).toBe("Researcher, MIT");
    expect(affiliation({ jobTitle: null, institution: "MIT" })).toBe("MIT");
    expect(affiliation({ jobTitle: null, institution: null })).toBe("");
  });
});
