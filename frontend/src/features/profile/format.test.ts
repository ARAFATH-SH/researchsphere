import { affiliation, countryName, yearRange } from "./format";

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
