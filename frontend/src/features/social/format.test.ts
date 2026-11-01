import { connectedLine, sentLine, shortDate } from "./format";

const now = new Date(2026, 10, 1, 15, 0);

describe("network dates", () => {
  it("writes a day as the design does", () => {
    expect(shortDate(new Date(2026, 8, 21))).toBe("21 Sep 2026");
  });

  it.each([
    [new Date(2026, 10, 1, 9, 0), "Sent today · Pending"],
    [new Date(2026, 9, 31, 23, 0), "Sent yesterday · Pending"],
    [new Date(2026, 9, 30, 12, 0), "Sent 2 days ago · Pending"],
    [new Date(2026, 9, 2, 12, 0), "Sent on 2 Oct 2026 · Pending"],
  ])("says when a request was sent: %s", (when, line) => {
    expect(sentLine(when.toISOString(), now)).toBe(line);
  });

  it("says when a connection was made", () => {
    expect(connectedLine(new Date(2026, 10, 1, 8, 0).toISOString(), now)).toBe("Connected today");
    expect(connectedLine(new Date(2026, 9, 3, 12, 0).toISOString(), now)).toBe("Connected 3 Oct 2026");
  });
});
