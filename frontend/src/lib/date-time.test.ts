import { dateAndTime, dateOnly } from "./date-time";

describe("date and time", () => {
  it("writes a day as the design does", () => {
    expect(dateOnly("2026-09-03T10:00:00")).toBe("3 Sep 2026");
  });

  it("adds the time of day", () => {
    expect(dateAndTime("2026-12-03T14:05:00")).toBe("3 Dec 2026, 2:05 PM");
  });
});
