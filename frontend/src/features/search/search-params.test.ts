import { queryTooShort, readSearch, searchHref } from "./search-params";

describe("readSearch", () => {
  it("reads every parameter", () => {
    expect(
      readSearch(new URLSearchParams("q=+ada+&institution=BUET&interest=ml&interest=nlp&page=3")),
    ).toEqual({ q: "ada", institution: "BUET", interests: ["ml", "nlp"], page: 3 });
  });

  it.each(["0", "51", "2.5", "x", "-1"])("turns page %j into 1", (page) => {
    expect(readSearch(new URLSearchParams(`q=ada&page=${page}`)).page).toBe(1);
  });

  it("defaults to an empty search", () => {
    expect(readSearch(new URLSearchParams())).toEqual({ q: "", institution: "", interests: [], page: 1 });
  });
});

describe("searchHref", () => {
  it("leaves out empty filters and page 1", () => {
    expect(searchHref({ q: "ada l", institution: "", interests: [], page: 1 })).toBe("/search?q=ada+l");
  });

  it("repeats interest and round-trips through readSearch", () => {
    const state = { q: "ada", institution: "BUET", interests: ["ml", "nlp"], page: 2 };
    const href = searchHref(state);
    expect(href).toBe("/search?q=ada&institution=BUET&interest=ml&interest=nlp&page=2");
    expect(readSearch(new URLSearchParams(href.split("?")[1]))).toEqual(state);
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
