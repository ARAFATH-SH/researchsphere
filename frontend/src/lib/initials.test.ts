import { initials } from "./initials";

describe("initials", () => {
  it.each([
    ["Ada King Lovelace", "AL"],
    ["ada", "A"],
    ["  Émile   Zola ", "ÉZ"],
    ["", "?"],
    ["   ", "?"],
  ])("%j gives %j", (name, want) => expect(initials(name)).toBe(want));
});
