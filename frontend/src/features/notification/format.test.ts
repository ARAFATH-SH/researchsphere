import { relativeTime } from "./format";

describe("relativeTime", () => {
  const now = new Date(2026, 10, 3, 15, 0, 0);
  const ago = (ms: number) => new Date(now.getTime() - ms).toISOString();
  const min = 60_000;
  const hour = 60 * min;

  it.each([
    ["a few seconds ago", ago(20_000), "Just now"],
    ["one minute ago", ago(min), "1 min ago"],
    ["59 minutes ago", ago(59 * min), "59 min ago"],
    ["an hour ago", ago(hour), "1 h ago"],
    ["23 hours ago, yesterday by the calendar", ago(23 * hour), "23 h ago"],
    ["yesterday, more than 24 hours ago", new Date(2026, 10, 2, 9, 0).toISOString(), "Yesterday"],
    ["two days ago", new Date(2026, 10, 1, 18, 0).toISOString(), "2 days ago"],
    ["six days ago", new Date(2026, 9, 28, 8, 0).toISOString(), "6 days ago"],
    ["seven days ago", new Date(2026, 9, 27, 8, 0).toISOString(), "27 Oct 2026"],
  ])("%s reads %p", (_, iso, want) => {
    expect(relativeTime(iso, now)).toBe(want);
  });
});
