import { countdown, countdownClass, dateRangeLabel, deadlineLabel, tileParts, userTimeLabel, zoneLabel } from "./time";

// 15 Feb 2027 23:59:59 AoE (UTC-12) is 16 Feb 11:59:59 UTC, 17:59:59 in Dhaka (UTC+6).
const aoe = { local: "2027-02-15T23:59:59", at: "2027-02-16T11:59:59Z" };

describe("deadlineLabel", () => {
  it("shows the AoE deadline and the same instant in Dhaka, on the next day", () => {
    expect(deadlineLabel(aoe, "AoE", "Asia/Dhaka")).toBe("15 Feb 2027, 23:59 AoE = 16 Feb, 17:59 Dhaka");
  });

  it.each([
    ["UTC", "15 Feb 2027, 23:59 AoE = 16 Feb, 11:59 UTC"],
    ["America/New_York", "15 Feb 2027, 23:59 AoE = 16 Feb, 06:59 New York"],
    // UTC-11 is one hour ahead of AoE: still past midnight on the 16th.
    ["Pacific/Pago_Pago", "15 Feb 2027, 23:59 AoE = 16 Feb, 00:59 Pago Pago"],
    // UTC+14: the same instant is already the 17th.
    ["Pacific/Kiritimati", "15 Feb 2027, 23:59 AoE = 17 Feb, 01:59 Kiritimati"],
  ])("reads the AoE boundary in %s", (zone, want) => {
    expect(deadlineLabel(aoe, "AoE", zone)).toBe(want);
  });

  it("one minute before the AoE deadline is still the 16th in Dhaka", () => {
    expect(userTimeLabel({ local: "2027-02-15T23:58:00", at: "2027-02-16T11:58:00Z" }, "AoE", "Asia/Dhaka")).toBe(
      "16 Feb, 17:58 Dhaka",
    );
  });

  it("adds the year when the reader's day is in another year", () => {
    const t = { local: "2026-12-31T23:59:59", at: "2027-01-01T11:59:59Z" };
    expect(deadlineLabel(t, "AoE", "Asia/Dhaka")).toBe("31 Dec 2026, 23:59 AoE = 1 Jan 2027, 17:59 Dhaka");
  });

  it("shows the time once when the reader is in the venue's zone", () => {
    const t = { local: "2027-03-01T17:00:00", at: "2027-03-01T11:00:00Z" };
    expect(deadlineLabel(t, "Asia/Dhaka", "Asia/Dhaka")).toBe("1 Mar 2027, 17:00 Dhaka");
    expect(deadlineLabel({ local: "2027-03-01T11:00:00", at: "2027-03-01T11:00:00Z" }, "UTC", "Etc/UTC")).toBe(
      "1 Mar 2027, 11:00 UTC",
    );
  });

  it("keeps the venue's wall time with daylight saving", () => {
    // 1 Jul 2027 23:59 in Berlin (UTC+2 in summer) is 21:59 UTC, 03:59 the next day in Dhaka.
    const t = { local: "2027-07-01T23:59:00", at: "2027-07-01T21:59:00Z" };
    expect(deadlineLabel(t, "Europe/Berlin", "Asia/Dhaka")).toBe("1 Jul 2027, 23:59 Berlin = 2 Jul, 03:59 Dhaka");
  });
});

describe("zoneLabel and tileParts", () => {
  it.each([
    ["AoE", "AoE"],
    ["UTC", "UTC"],
    ["Asia/Dhaka", "Dhaka"],
    ["America/Argentina/Buenos_Aires", "Buenos Aires"],
  ])("%s is %s", (zone, want) => expect(zoneLabel(zone)).toBe(want));

  it("takes the tile from the venue's own date", () => {
    expect(tileParts(aoe)).toEqual({ month: "FEB", day: "15" });
  });
});

describe("countdown", () => {
  const now = new Date("2027-02-01T00:00:00Z");
  const after = (ms: number) => new Date(now.getTime() + ms).toISOString();
  const day = 86_400_000;

  it.each([
    [12 * day, "in 12 days", "normal"],
    [7 * day, "in 7 days", "normal"],
    [7 * day - 1, "in 6 days", "urgent"],
    [day, "in 1 day", "urgent"],
    [5 * 3_600_000, "in 5 hours", "urgent"],
    [90_000, "in 2 minutes", "urgent"],
    [0, "closed just now", "past"],
    [-3 * day, "closed 3 days ago", "past"],
    [-2 * 3_600_000, "closed 2 hours ago", "past"],
  ])("%d ms left is %j (%s)", (ms, text, tone) => {
    expect(countdown(after(ms), now)).toEqual({ text, tone });
  });

  it("is red under 7 days and muted once past", () => {
    expect(countdownClass.urgent).toBe("text-rs-danger");
    expect(countdownClass.normal).not.toContain("danger");
    expect(countdownClass.past).toBe("text-rs-text-muted");
  });
});

describe("dateRangeLabel", () => {
  it.each([
    ["2027-02-20", null, "20 Feb 2027"],
    ["2027-03-12", "2027-03-14", "12–14 Mar 2027"],
    ["2027-03-30", "2027-04-02", "30 Mar – 2 Apr 2027"],
    ["2026-12-30", "2027-01-02", "30 Dec 2026 – 2 Jan 2027"],
    [null, null, ""],
  ])("%s to %s", (s, e, want) => expect(dateRangeLabel(s, e)).toBe(want));
});
