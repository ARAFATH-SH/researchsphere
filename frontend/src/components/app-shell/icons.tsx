// Outline icons for the top bar, drawn as in design/screens/: 24px grid, 1.8px stroke,
// currentColor. Decorative: the tab's text label names it.
import type { ReactNode } from "react";

function Icon({ children }: { children: ReactNode }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className="size-6 flex-none"
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

export const HomeIcon = () => (
  <Icon>
    <path d="M3 11l9-7 9 7v9a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1z" />
  </Icon>
);

export const NetworkIcon = () => (
  <Icon>
    <circle cx="9" cy="8" r="3.5" />
    <path d="M2.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6" />
    <circle cx="17" cy="9" r="2.5" />
    <path d="M17 14c2.6 0 4.5 1.8 4.5 4.5" />
  </Icon>
);

export const ProjectsIcon = () => (
  <Icon>
    <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
  </Icon>
);

export const MessagingIcon = () => (
  <Icon>
    <path d="M4 5h16v11H8l-4 4z" />
  </Icon>
);

export const NotificationsIcon = () => (
  <Icon>
    <path d="M6 16V11a6 6 0 0 1 12 0v5l2 2H4z" />
    <path d="M10 20a2 2 0 0 0 4 0" />
  </Icon>
);

export const EventsIcon = () => (
  <Icon>
    <rect x="3" y="5" width="18" height="16" rx="2" />
    <path d="M3 10h18M8 3v4M16 3v4" />
  </Icon>
);

export const PostIcon = () => (
  <Icon>
    <rect x="3" y="3" width="18" height="18" rx="3" />
    <path d="M12 8v8M8 12h8" />
  </Icon>
);

// MeIcon stands in for the photo while the session loads.
export const MeIcon = () => (
  <Icon>
    <circle cx="12" cy="12" r="9" />
    <circle cx="12" cy="10" r="3" />
    <path d="M6.5 18.2a6 6 0 0 1 11 0" />
  </Icon>
);

export const ChevronDownIcon = () => (
  <svg viewBox="0 0 24 24" className="size-3" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M6 9l6 6 6-6" />
  </svg>
);

export const SearchIcon = () => (
  <svg viewBox="0 0 24 24" className="size-4 flex-none" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
    <circle cx="11" cy="11" r="7" />
    <path d="M20 20l-4-4" />
  </svg>
);
