import type { Profile } from "./api";
import type { EditSection } from "./edit-button";

// StrengthItem is one line of the profile checklist: what it checks, whether the profile
// has it, and the prompt shown while it is missing.
// section is the edit dialog that fills it in.
export type StrengthItem = { label: string; action: string; done: boolean; section: EditSection };

// minTags is how many interests or skills count as a complete list.
export const minTags = 3;

// profileStrength checks the parts of a profile that help others find and judge it,
// computed from the profile alone. percent is rounded to a whole number.
export function profileStrength(p: Profile): { items: StrengthItem[]; percent: number } {
  const items: StrengthItem[] = [
    { label: "Headline", action: "Add a headline", done: Boolean(p.headline), section: "intro" },
    { label: "Bio", action: "Write a bio", done: Boolean(p.bio), section: "about" },
    { label: "Institution", action: "Add your institution", done: Boolean(p.institution), section: "intro" },
    {
      label: `${minTags}+ research interests`,
      action: `Add ${minTags} or more research interests`,
      done: p.interests.length >= minTags,
      section: "interests",
    },
    {
      label: `${minTags}+ skills`,
      action: `Add ${minTags} or more skills`,
      done: p.skills.length >= minTags,
      section: "skills",
    },
    { label: "Education", action: "Add your education", done: p.education.length > 0, section: "education" },
  ];
  const done = items.filter((i) => i.done).length;
  return { items, percent: Math.round((done / items.length) * 100) };
}
