import { splitLinks } from "./links";

describe("splitLinks", () => {
  it("leaves plain text alone", () => {
    expect(splitLinks("no links here")).toEqual([{ text: "no links here" }]);
  });

  it("finds http and https links among text", () => {
    expect(splitLinks("see http://a.org and https://b.org/x?y=1 now")).toEqual([
      { text: "see " },
      { text: "http://a.org", href: "http://a.org" },
      { text: " and " },
      { text: "https://b.org/x?y=1", href: "https://b.org/x?y=1" },
      { text: " now" },
    ]);
  });

  it("leaves sentence punctuation out of a link", () => {
    expect(splitLinks("Read https://a.org/p.")).toEqual([{ text: "Read " }, { text: "https://a.org/p", href: "https://a.org/p" }, { text: "." }]);
  });

  it.each(["javascript:alert(1)", "data:text/html,<script>alert(1)</script>", "ftp://a.org/x", "www.a.org"])(
    "does not link %s",
    (body) => {
      expect(splitLinks(body).some((p) => p.href)).toBe(false);
    },
  );

  it("ends a link at a tag or a quote", () => {
    const parts = splitLinks('https://a.org/<img src=x onerror=alert(1)>');
    expect(parts[0]).toEqual({ text: "https://a.org/", href: "https://a.org/" });
    expect(parts.map((p) => p.text).join("")).toBe('https://a.org/<img src=x onerror=alert(1)>');
  });
});
