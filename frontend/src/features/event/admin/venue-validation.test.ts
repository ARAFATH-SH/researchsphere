import { venue } from "../test-venues";
import { emptyForm, formOf, localTime, shows, toInput, toPatch, urlError, validate, validZone, type VenueForm } from "./venue-validation";

const good: VenueForm = {
  ...emptyForm,
  name: "Workshop on Graph Learning",
  type: "workshop",
  fields: ["explainable_ai"],
  attendance: "online",
  timeZone: "AoE",
  submissionDeadline: "2027-03-01T23:59",
  officialUrl: "https://glw.example.org",
  scope: "Graph learning.",
};

describe("validate", () => {
  it("accepts a complete online workshop", () => {
    expect(validate(good)).toEqual({});
  });

  it.each<[string, Partial<VenueForm>, string]>([
    ["name", { name: "  " }, "name"],
    ["name on two lines", { name: "A\nB" }, "name"],
    ["name too long", { name: "x".repeat(301) }, "name"],
    ["short name too long", { shortName: "x".repeat(41) }, "shortName"],
    ["no field", { fields: [] }, "fields"],
    ["six fields", { fields: ["a", "b", "c", "d", "e", "f"] }, "fields"],
    ["no attendance for a workshop", { attendance: "" }, "attendance"],
    ["no city in person", { attendance: "in_person", country: "BD" }, "city"],
    ["a three-letter country", { attendance: "hybrid", city: "Dhaka", country: "BGD" }, "country"],
    ["ends before it starts", { startsOn: "2027-05-10", endsOn: "2027-05-09" }, "endsOn"],
    ["an unknown zone", { timeZone: "Mars/Olympus" }, "timeZone"],
    ["no deadline", { submissionDeadline: "" }, "submissionDeadline"],
    ["abstract after the deadline", { abstractDeadline: "2027-03-02T00:00" }, "abstractDeadline"],
    ["notification before the deadline", { notificationAt: "2027-03-01T12:00" }, "notificationAt"],
    ["an http link", { officialUrl: "http://glw.example.org" }, "officialUrl"],
    ["a link with a user name", { officialUrl: "https://me:pw@glw.example.org" }, "officialUrl"],
    ["no scope", { scope: " " }, "scope"],
    ["scope over 10,000", { scope: "x".repeat(10_001) }, "scope"],
    ["a control character in the scope", { scope: "bell\u0007" }, "scope"],
  ])("refuses %s", (_, change, field) => {
    expect(Object.keys(validate({ ...good, ...change }))).toEqual([field]);
  });

  it("keeps line breaks and tabs in the scope, and 10,000 characters", () => {
    expect(validate({ ...good, scope: "a\n\tb" })).toEqual({});
    expect(validate({ ...good, scope: "x".repeat(10_000) })).toEqual({});
  });

  it("asks a rolling journal for no zone and no deadline", () => {
    const rolling: VenueForm = { ...good, type: "journal", rolling: true, attendance: "", timeZone: "", submissionDeadline: "" };
    expect(validate(rolling)).toEqual({});
    expect(toInput(rolling)).toMatchObject({ rolling: true, timeZone: null, submissionDeadline: null, attendance: null });
  });

  it("knows AoE, UTC and IANA names", () => {
    expect(validZone("AoE")).toBe(true);
    expect(validZone("UTC")).toBe(true);
    expect(validZone("Asia/Dhaka")).toBe(true);
    expect(validZone("aoe")).toBe(false);
    expect(urlError("https://x.example.org/cfp?y=1")).toBe("");
  });
});

describe("shows and toInput", () => {
  it("has no place, dates or attendance for a journal, and rolling only for a journal", () => {
    expect(shows({ ...good, type: "journal" })).toMatchObject({ attendance: false, place: false, dates: false, rolling: true });
    expect(shows({ ...good, type: "conference", attendance: "hybrid" })).toMatchObject({ place: true, rolling: false });
    // A workshop's rolling flag is never sent as true.
    expect(toInput({ ...good, rolling: true }).rolling).toBe(false);
  });

  it("sends null for what does not apply and an upper-case country", () => {
    const input = toInput({ ...good, attendance: "in_person", city: " Dhaka ", country: "bd", shortName: " " });
    expect(input).toMatchObject({ city: "Dhaka", country: "BD", shortName: null, abstractDeadline: null });
    expect(toInput({ ...good, city: "Dhaka", country: "BD" })).toMatchObject({ city: null, country: null });
  });
});

describe("toPatch", () => {
  it("sends only the changed fields", () => {
    const v = venue();
    expect(toPatch(v, formOf(v))).toEqual({});
    expect(toPatch(v, { ...formOf(v), scope: "New scope." })).toEqual({ scope: "New scope." });
  });

  it("clears an optional field with null and treats HH:MM as HH:MM:00", () => {
    const v = venue({ abstractDeadline: { local: "2027-02-08T23:59:00", at: "2027-02-09T11:59:00Z" } });
    expect(toPatch(v, { ...formOf(v), shortName: "", abstractDeadline: "2027-02-08T23:59" })).toEqual({ shortName: null });
    expect(toPatch(v, { ...formOf(v), abstractDeadline: "" })).toEqual({ abstractDeadline: null });
  });

  it("sends the new zone and the fields as a whole list", () => {
    const v = venue();
    expect(toPatch(v, { ...formOf(v), timeZone: "UTC", fields: ["bangla_nlp", "health_informatics"] })).toEqual({
      timeZone: "UTC",
      fields: ["bangla_nlp", "health_informatics"],
    });
  });
});

it("drops fractions of a second from a datetime-local value", () => {
  expect(localTime("2027-03-01T23:59:59.000")).toBe("2027-03-01T23:59:59");
  expect(localTime("2027-03-01T23:59")).toBe("2027-03-01T23:59");
});
