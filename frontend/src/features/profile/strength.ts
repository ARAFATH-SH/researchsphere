import type { Profile } from "./api";

// StrengthItem is one line of the profile checklist: what it checks, whether the profile
// has it, and the prompt shown while it is missing.
export type StrengthItem = { label: string; action: string; done: boolean };

// minTags is how many interests or skills count as a complete list.
export const minTags = 3;

// profileStrength checks the parts of a profile that help others find and judge it,
// computed from the profile alone. percent is rounded to a whole number.
export function profileStrength(p: Profile): { items: StrengthItem[]; percent: number } {
  const items: StrengthItem[] = [
    { label: "Headline", action: "Add a headline", done: Boolean(p.headline) },
    { label: "Bio", action: "Write a bio", done: Boolean(p.bio) },
    { label: "Institution", action: "Add your institution", done: Boolean(p.institution) },
    {
      label: `${minTags}+ research interests`,
      action: `Add ${minTags} or more research interests`,
      done: p.interests.length >= minTags,
    },
    { label: `${minTags}+ skills`, action: `Add ${minTags} or more skills`, done: p.skills.length >= minTags },
    { label: "Education", action: "Add your education", done: p.education.length > 0 },
  ];
  const done = items.filter((i) => i.done).length;
  return { items, percent: Math.round((done / items.length) * 100) };
}
