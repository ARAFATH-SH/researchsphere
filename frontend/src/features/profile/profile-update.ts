import type { Profile, ProfileUpdate } from "./api";

// profileUpdate builds the body of PUT /profiles/me, which replaces every field: the
// stored values, with the changed ones on top, so a dialog for one card leaves the
// others as they are.
export function profileUpdate(p: Profile, changes: Partial<ProfileUpdate>): ProfileUpdate {
  return {
    fullName: p.fullName,
    headline: p.headline,
    bio: p.bio,
    institution: p.institution,
    department: p.department,
    jobTitle: p.jobTitle,
    country: p.country,
    ...changes,
  };
}

// orNull trims a text field and turns an empty one into null ("not set").
export function orNull(value: string): string | null {
  const v = value.trim();
  return v === "" ? null : v;
}
