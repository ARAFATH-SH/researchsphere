import type { Profile } from "./api";

// Profiles shared by the profile tests.

export const adaID = "0b8f4c1e-0000-4000-8000-000000000001";
export const bobID = "0b8f4c1e-0000-4000-8000-000000000002";

// emptyProfile has only a name: every optional field null and every list empty.
export function emptyProfile(userId: string, fullName: string): Profile {
  return {
    userId,
    fullName,
    headline: null,
    bio: null,
    institution: null,
    department: null,
    jobTitle: null,
    country: null,
    interests: [],
    skills: [],
    education: [],
    publications: [],
    connectionStatus: "none",
    contact: { links: [] },
  };
}

// fullProfile has every section filled in.
export function fullProfile(userId: string, fullName: string): Profile {
  return {
    ...emptyProfile(userId, fullName),
    headline: "Analyst of engines",
    bio: "First line.\nSecond line.",
    institution: "University of London",
    department: "Mathematics",
    jobTitle: "Researcher",
    country: "GB",
    interests: [
      { id: "i1", name: "Machine Learning" },
      { id: "i2", name: "Robotics" },
      { id: "i3", name: "NLP" },
    ],
    skills: [
      { id: "s1", name: "Go" },
      { id: "s2", name: "Python" },
      { id: "s3", name: "LaTeX" },
    ],
    education: [
      { id: "e1", institution: "ETH Zürich", degree: "PhD", fieldOfStudy: "Computer Science", startYear: 2026, endYear: null },
      { id: "e2", institution: "MIT", degree: "BSc", fieldOfStudy: null, startYear: 2022, endYear: 2026 },
    ],
  };
}
