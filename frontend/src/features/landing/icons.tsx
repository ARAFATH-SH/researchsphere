// Outline icons for the landing page, drawn for this project (docs/ui-guidelines.md, Icons):
// 24px grid, 1.75px stroke, currentColor. Decorative: the text next to each names it.
import type { ReactNode } from "react";

function Icon({ children }: { children: ReactNode }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className="h-6 w-6"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.75"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

export const ClockIcon = () => (
  <Icon>
    <circle cx="12" cy="12" r="8.5" />
    <path d="M12 7.5V12l3 2" />
  </Icon>
);

export const MailIcon = () => (
  <Icon>
    <rect x="3.5" y="5.5" width="17" height="13" rx="2" />
    <path d="m4 7 8 6 8-6" />
  </Icon>
);

export const DiceIcon = () => (
  <Icon>
    <rect x="4" y="4" width="16" height="16" rx="3" />
    <circle cx="9" cy="9" r="0.75" fill="currentColor" />
    <circle cx="15" cy="15" r="0.75" fill="currentColor" />
    <circle cx="12" cy="12" r="0.75" fill="currentColor" />
  </Icon>
);

export const ProfileIcon = () => (
  <Icon>
    <rect x="3.5" y="4.5" width="17" height="15" rx="2" />
    <circle cx="9" cy="11" r="2.5" />
    <path d="M5.5 17a3.5 3.5 0 0 1 7 0M15 9.5h3M15 13h3" />
  </Icon>
);

export const SparkIcon = () => (
  <Icon>
    <path d="M12 3.5 13.8 10.2 20.5 12l-6.7 1.8L12 20.5l-1.8-6.7L3.5 12l6.7-1.8Z" />
  </Icon>
);

export const DocumentIcon = () => (
  <Icon>
    <path d="M6.5 3.5h7l4 4v13h-11Z" />
    <path d="M13.5 3.5v4h4M9 12h6M9 15.5h6" />
  </Icon>
);

export const StudentIcon = () => (
  <Icon>
    <path d="m2.5 9 9.5-4.5L21.5 9 12 13.5Z" />
    <path d="M6.5 11v4.5c0 1.5 2.5 3 5.5 3s5.5-1.5 5.5-3V11M21.5 9v5" />
  </Icon>
);

export const PeopleIcon = () => (
  <Icon>
    <circle cx="9" cy="8.5" r="3" />
    <path d="M3.5 19a5.5 5.5 0 0 1 11 0" />
    <circle cx="16.5" cy="9.5" r="2.5" />
    <path d="M16 14a4.5 4.5 0 0 1 4.5 5" />
  </Icon>
);

export const FlaskIcon = () => (
  <Icon>
    <path d="M9.5 3.5h5M10 3.5v6l-5 8.5a1.5 1.5 0 0 0 1.3 2.5h11.4a1.5 1.5 0 0 0 1.3-2.5L14 9.5v-6" />
    <path d="M7.5 15h9" />
  </Icon>
);

// CheckIcon marks a bullet point; it is smaller than the 24px icons above.
export const CheckIcon = () => (
  <svg
    viewBox="0 0 24 24"
    className="mt-0.5 h-5 w-5 shrink-0 text-primary"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden="true"
  >
    <path d="m5 12.5 4.5 4.5L19 7.5" />
  </svg>
);

// The design's 28px feature-card icons (design/screens/Landing.html): 1.8px stroke.
function FeatureIcon({ children }: { children: ReactNode }) {
  return (
    <svg
      viewBox="0 0 24 24"
      width="28"
      height="28"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

export const CollaboratorsIcon = () => (
  <FeatureIcon>
    <circle cx="9" cy="8" r="3.5" />
    <path d="M2.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6" />
    <circle cx="17" cy="9" r="2.5" />
    <path d="M17 14c2.6 0 4.5 1.8 4.5 4.5" />
  </FeatureIcon>
);

export const CodeIcon = () => (
  <FeatureIcon>
    <path d="M8 6l-5 6 5 6M16 6l5 6-5 6" />
  </FeatureIcon>
);

export const CalendarIcon = () => (
  <FeatureIcon>
    <rect x="3" y="5" width="18" height="16" rx="2" />
    <path d="M3 10h18M8 3v4M16 3v4" />
  </FeatureIcon>
);
