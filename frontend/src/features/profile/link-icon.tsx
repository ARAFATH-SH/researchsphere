import type { ReactNode } from "react";

import type { LinkKind } from "./api";

// Plain icons for each link kind, drawn for this project: 24px, 1.75px stroke,
// currentColor. They hint at the kind; they are not the sites' logos.
const paths: Record<LinkKind, ReactNode> = {
  // a globe
  website: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18" />
    </>
  ),
  // a graduation cap
  google_scholar: (
    <>
      <path d="m2 9 10-5 10 5-10 5L2 9Z" />
      <path d="M6 11v5c3 2 9 2 12 0v-5M22 9v5" />
    </>
  ),
  // a flask
  researchgate: (
    <path d="M9 3h6M10 3v6l-5 9a2 2 0 0 0 1.7 3h10.6a2 2 0 0 0 1.7-3l-5-9V3M7.5 15h9" />
  ),
  // a briefcase
  linkedin: (
    <>
      <rect x="3" y="7" width="18" height="13" rx="2" />
      <path d="M9 7V5a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2M3 13h18" />
    </>
  ),
  // code brackets
  github: <path d="m8 7-5 5 5 5M16 7l5 5-5 5M14 4l-4 16" />,
  // a chain link
  other: (
    <path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1" />
  ),
};

export function LinkIcon({ kind }: { kind: LinkKind }) {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="size-6 shrink-0"
    >
      {paths[kind]}
    </svg>
  );
}
