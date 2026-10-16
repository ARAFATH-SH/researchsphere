// Outline icons drawn for this project (docs/ui-guidelines.md, Icons): 24px grid,
// 1.75px stroke, currentColor. Decorative: the tab's text label names it.
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

export const HomeIcon = () => (
  <Icon>
    <path d="M3.5 11 12 4l8.5 7" />
    <path d="M5.5 9.5V20h5v-5h3v5h5V9.5" />
  </Icon>
);

export const NetworkIcon = () => (
  <Icon>
    <circle cx="9" cy="8" r="3" />
    <path d="M3.5 19a5.5 5.5 0 0 1 11 0" />
    <circle cx="17" cy="9.5" r="2.25" />
    <path d="M15.5 14.2A4.5 4.5 0 0 1 21 18.5" />
  </Icon>
);

export const ProjectsIcon = () => (
  <Icon>
    <path d="M3.5 7.5a2 2 0 0 1 2-2h4l2 2h7a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z" />
    <path d="M8 13h8M8 16h5" />
  </Icon>
);

export const EventsIcon = () => (
  <Icon>
    <rect x="3.5" y="5" width="17" height="15" rx="2" />
    <path d="M3.5 9.5h17M8 3v4M16 3v4" />
    <path d="M8 13.5h2M14 13.5h2M8 16.5h2" />
  </Icon>
);

export const NotificationsIcon = () => (
  <Icon>
    <path d="M6 16.5V11a6 6 0 0 1 12 0v5.5l1.5 1.5h-15z" />
    <path d="M10 20.5a2 2 0 0 0 4 0" />
  </Icon>
);

export const MeIcon = () => (
  <Icon>
    <circle cx="12" cy="12" r="9" />
    <circle cx="12" cy="10" r="3" />
    <path d="M6.5 18.2a6 6 0 0 1 11 0" />
  </Icon>
);

export const SearchIcon = ({ className = "h-4 w-4" }: { className?: string }) => (
  <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
    <circle cx="11" cy="11" r="6.5" />
    <path d="m16 16 4.5 4.5" />
  </svg>
);

export const AdminIcon = () => (
  <Icon>
    <path d="M12 3.5 19 6v5.5c0 4.3-2.9 7.6-7 9-4.1-1.4-7-4.7-7-9V6z" />
    <path d="m9 12 2 2 4-4" />
  </Icon>
);
