import Link from "next/link";

// LogoSquare is ResearchSphere's mark from the design: "RS" on a teal square. sm is the
// footer size. It is decorative; the link around it carries the name.
export function LogoSquare({ size = "md" }: { size?: "sm" | "md" }) {
  const box = size === "sm" ? "size-[22px] rounded-rs-chip text-rs-micro" : "size-[34px] rounded-rs-xs text-rs-body";
  return (
    <span
      aria-hidden="true"
      className={`flex flex-none items-center justify-center bg-rs-accent font-bold text-rs-surface ${box}`}
    >
      RS
    </span>
  );
}

const focusRing = "rounded-rs-xs focus-visible:shadow-focus focus-visible:outline-none";

// Logo is the square and the name, linking to href. markOnly shows the square alone (the
// signed-in top bar); the name stays available to screen readers.
export function Logo({ href = "/", markOnly = false }: { href?: string; markOnly?: boolean }) {
  if (markOnly) {
    return (
      <Link href={href} aria-label="ResearchSphere home" className={`flex-none ${focusRing}`}>
        <LogoSquare />
      </Link>
    );
  }
  return (
    <Link
      href={href}
      className={`flex flex-none items-center gap-2 text-rs-title-sm font-bold text-rs-accent ${focusRing}`}
    >
      <LogoSquare />
      ResearchSphere
    </Link>
  );
}
