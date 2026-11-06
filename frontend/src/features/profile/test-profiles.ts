import type { Profile } from "./api";

// Profiles shared by the profile tests.

export const adaID = "0b8f4c1e-0000-4000-8000-000000000001";
export const bobID = "0b8f4c1e-0000-4000-8000-000000000002";

// emptyProfile has only a name: every optional field null and every list empty. The tests
// sign in as Ada, so her profile reads as "self", as the API answers, and anyone else's
// as "none".
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
    city: null,
    orcidId: null,
    interests: [],
    skills: [],
    education: [],
    experience: [],
    languages: [],
    publicationCount: 0,
    connectionStatus: userId === adaID ? "self" : "none",
    connectionRequestId: null,
    connectionCount: 0,
    conversation: null,
    contact: { links: [] },
    photoUrl: null,
    coverUrl: null,
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
      {
        id: "e1",
        institution: "ETH Zürich",
        degree: "PhD",
        fieldOfStudy: "Computer Science",
        startYear: 2026,
        endYear: null,
        grade: null,
        description: null,
      },
      {
        id: "e2",
        institution: "MIT",
        degree: "BSc",
        fieldOfStudy: null,
        startYear: 2022,
        endYear: 2026,
        grade: null,
        description: null,
      },
    ],
    experience: [
      {
        id: "x1",
        title: "Research Assistant",
        organization: "University of Dhaka",
        employmentType: "research_assistant",
        location: "Dhaka, Bangladesh",
        locationType: "on_site",
        startDate: { year: 2025, month: 3 },
        endDate: null,
        isCurrent: true,
        description: "Built the lab's data pipeline.\nTaught two tutorials.",
      },
      {
        id: "x2",
        title: "Teaching Assistant",
        organization: "MIT",
        employmentType: "teaching_assistant",
        location: null,
        locationType: null,
        startDate: { year: 2023, month: 1 },
        endDate: { year: 2024, month: 12 },
        isCurrent: false,
        description: null,
      },
    ],
  };
}
