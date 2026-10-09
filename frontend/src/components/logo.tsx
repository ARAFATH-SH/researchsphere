import Link from "next/link";

// LogoMark is ResearchSphere's own mark: a sphere with an orbit and two linked nodes
// (people working together). Drawn for this project; it copies no other logo.
export function LogoMark({ className = "h-8 w-8" }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={className} aria-hidden="true">
      <rect width="32" height="32" rx="8" className="fill-primary" />
      <circle cx="16" cy="16" r="8.5" fill="none" className="stroke-surface" strokeWidth="2" />
      <ellipse cx="16" cy="16" rx="12" ry="4.5" fill="none" className="stroke-surface" strokeWidth="1.5" transform="rotate(-25 16 16)" opacity="0.85" />
      <circle cx="9.5" cy="11" r="2.2" className="fill-surface" />
      <circle cx="22.5" cy="21" r="2.2" className="fill-surface" />
    </svg>
  );
}

// Below each breakpoint only the mark shows; the name stays available to screen readers.
const compactClass = {
  none: "",
  sm: "sr-only sm:not-sr-only",
  lg: "sr-only lg:not-sr-only",
};

// Logo is the mark and the wordmark, linking to href. compactBelow hides the wordmark on
// screens narrower than that breakpoint, to leave room for other controls.
export function Logo({ href = "/", compactBelow = "none" }: { href?: string; compactBelow?: keyof typeof compactClass }) {
  return (
    <Link
      href={href}
      className="flex items-center gap-2 rounded-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
    >
      <LogoMark />
      <span className={`text-xl font-bold tracking-tight text-secondary ${compactClass[compactBelow]}`}>
        ResearchSphere
      </span>
    </Link>
  );
}
