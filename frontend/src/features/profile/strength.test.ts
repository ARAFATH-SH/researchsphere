import { profileStrength } from "./strength";
import { adaID, emptyProfile, fullProfile } from "./test-profiles";

describe("profileStrength", () => {
  it("is 0% with every item missing for a profile with only a name", () => {
    const { items, percent } = profileStrength(emptyProfile(adaID, "Ada"));
    expect(percent).toBe(0);
    expect(items.map((i) => [i.label, i.action])).toEqual([
      ["Headline", "Add a headline"],
      ["Bio", "Write a bio"],
      ["Institution", "Add your institution"],
      ["3+ research interests", "Add 3 or more research interests"],
      ["3+ skills", "Add 3 or more skills"],
      ["Education", "Add your education"],
    ]);
    expect(items.every((i) => !i.done)).toBe(true);
  });

  it("is 100% for a complete profile", () => {
    const { items, percent } = profileStrength(fullProfile(adaID, "Ada"));
    expect(percent).toBe(100);
    expect(items.every((i) => i.done)).toBe(true);
  });

  it("needs at least 3 interests and 3 skills", () => {
    const p = fullProfile(adaID, "Ada");
    p.interests = p.interests.slice(0, 2);
    p.skills = p.skills.slice(0, 2);
    const { items, percent } = profileStrength(p);
    expect(items.filter((i) => !i.done).map((i) => i.label)).toEqual(["3+ research interests", "3+ skills"]);
    expect(percent).toBe(67);
  });
});
