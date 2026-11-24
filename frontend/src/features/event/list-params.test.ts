import { activeFilterCount, eventsHref, rangeError, readEvents, toQuery } from "./list-params";

const read = (s: string) => readEvents(new URLSearchParams(s));

describe("readEvents and eventsHref", () => {
  it("reads every filter and writes the same URL back", () => {
    const url = "type=conference&type=workshop&field=bangla_nlp&field=other&from=2027-01-01&to=2027-03-31&past=1&q=health&page=3";
    const s = read(url);
    expect(s).toEqual({
      tab: "deadlines",
      types: ["conference", "workshop"],
      fields: ["bangla_nlp", "other"],
      from: "2027-01-01",
      to: "2027-03-31",
      q: "health",
      past: true,
      page: 3,
    });
    expect(eventsHref(s)).toBe(`/events?${url}`);
  });

  it("drops values that cannot be right", () => {
    expect(read("type=poster&field=Bad%20Key&from=tomorrow&page=0")).toMatchObject({
      types: [],
      fields: [],
      from: "",
      page: 1,
    });
  });

  it("leaves the deadline filters out of the journals tab", () => {
    const s = read("tab=journals&past=1&from=2027-01-01&type=journal");
    expect(s).toMatchObject({ tab: "journals", past: false, from: "", types: ["journal"] });
    expect(eventsHref(s)).toBe("/events?tab=journals&type=journal");
  });

  it("is /events with nothing set", () => {
    expect(eventsHref(read(""))).toBe("/events");
  });
});

describe("toQuery", () => {
  it("sends the deadline range with the reader's zone and past venues on request", () => {
    expect(toQuery(read("from=2027-01-01&past=1&q=a"), "Asia/Dhaka")).toMatchObject({
      rolling: false,
      includePast: true,
      deadlineFrom: "2027-01-01",
      tz: "Asia/Dhaka",
      q: undefined, // one character is too short to send
    });
  });

  it("asks for rolling journals only on the journals tab", () => {
    expect(toQuery(read("tab=journals&q=nature"), "UTC")).toMatchObject({
      rolling: true,
      includePast: false,
      deadlineFrom: undefined,
      q: "nature",
    });
  });
});

it("counts the filters set and checks the range", () => {
  expect(activeFilterCount(read("type=journal&field=other&past=1"))).toBe(3);
  expect(rangeError({ from: "2027-02-01", to: "2027-01-31" })).not.toBe("");
  expect(rangeError({ from: "2027-02-01", to: "2027-02-01" })).toBe("");
});
