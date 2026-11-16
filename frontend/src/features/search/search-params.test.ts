import { queryTooLong, queryTooShort, readSearch, searchHref } from "./search-params";

describe("readSearch", () => {
  it("reads every parameter", () => {
    expect(
      readSearch(new URLSearchParams("q=+ada+&institution=BUET&interest=ml&interest=nlp&page=3")),
    ).toEqual({ q: "ada", institution: "BUET", interests: ["ml", "nlp"], page: 3, keywordOnly: false, type: "people" });
  });

  it.each([
    ["projects", "projects"],
    ["people", "people"],
    ["posts", "people"],
  ])("reads type %j as the %s tab", (type, tab) => {
    expect(readSearch(new URLSearchParams(`q=ada&type=${type}`)).type).toBe(tab);
  });

  it.each(["0", "51", "2.5", "x", "-1"])("turns page %j into 1", (page) => {
    expect(readSearch(new URLSearchParams(`q=ada&page=${page}`)).page).toBe(1);
  });

  it("defaults to an empty search", () => {
    expect(readSearch(new URLSearchParams())).toEqual({
      q: "",
      institution: "",
      interests: [],
      page: 1,
      keywordOnly: false,
      type: "people",
    });
  });

  it.each([
    ["keyword", true],
    ["auto", false],
    ["other", false],
  ])("reads mode %j as keywordOnly %s", (mode, keywordOnly) => {
    expect(readSearch(new URLSearchParams(`q=ada&mode=${mode}`)).keywordOnly).toBe(keywordOnly);
  });
});

describe("searchHref", () => {
  it("leaves out empty filters and page 1", () => {
    expect(searchHref({ q: "ada l", institution: "", interests: [], page: 1, keywordOnly: false })).toBe(
      "/search?q=ada+l",
    );
  });

  it("repeats interest and round-trips through readSearch", () => {
    const state = {
      q: "ada",
      institution: "BUET",
      interests: ["ml", "nlp"],
      page: 2,
      keywordOnly: true,
      type: "people" as const,
    };
    const href = searchHref(state);
    expect(href).toBe("/search?q=ada&institution=BUET&interest=ml&interest=nlp&page=2&mode=keyword");
    expect(readSearch(new URLSearchParams(href.split("?")[1]))).toEqual(state);
  });
});

describe("searchHref for projects", () => {
  it("names the tab, leaves out the people filters and round-trips", () => {
    const href = searchHref({
      q: "graph",
      institution: "BUET",
      interests: ["ml"],
      page: 3,
      keywordOnly: true,
      type: "projects",
    });
    expect(href).toBe("/search?q=graph&type=projects&page=3&mode=keyword");
    expect(readSearch(new URLSearchParams(href.split("?")[1]))).toEqual({
      q: "graph",
      institution: "",
      interests: [],
      page: 3,
      keywordOnly: true,
      type: "projects",
    });
  });
});

describe("queryTooShort", () => {
  it.each([
    ["", true],
    [" a ", true],
    ["ab", false],
  ])("%j is too short: %s", (q, short) => {
    expect(queryTooShort(q)).toBe(short);
  });
});

describe("queryTooLong", () => {
  it.each([
    [300, false, false],
    [301, false, true],
    [100, true, false],
    [101, true, true],
  ])("%i characters with keywordOnly %s is too long: %s", (length, keywordOnly, long) => {
    expect(queryTooLong("a".repeat(length), keywordOnly)).toBe(long);
  });
});
