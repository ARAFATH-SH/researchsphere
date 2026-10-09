import Link from "next/link";

import { buttonClass } from "@/components/ui/button";

// The landing page: a two-column hero (docs/ui-guidelines.md, Signed-out pages).
export default function Page() {
  return (
    <section className="grid items-center gap-10 py-6 md:grid-cols-2 md:py-12">
      <div className="space-y-6">
        <h1 className="text-4xl leading-tight font-light text-secondary md:text-5xl">
          Find collaborators. Write papers together.
        </h1>
        <p className="max-w-prose text-lg text-text-muted">
          Build your research profile, meet researchers whose work fits yours, write in one shared
          LaTeX editor and find the right venue for your paper.
        </p>
        <div className="flex max-w-sm flex-col gap-3 sm:flex-row">
          <Link href="/register" className={buttonClass({ variant: "primary", full: true })}>
            Join now
          </Link>
          <Link href="/login" className={buttonClass({ variant: "outline", full: true })}>
            Sign in
          </Link>
        </div>
      </div>
      <HeroIllustration />
    </section>
  );
}

// HeroIllustration is a placeholder made of simple shapes: profile cards linked into a
// network around a shared document. Drawn for this project; no copied images.
function HeroIllustration() {
  return (
    <svg viewBox="0 0 400 320" className="mx-auto w-full max-w-md" aria-hidden="true">
      <circle cx="200" cy="160" r="150" className="fill-primary-soft" />
      <g className="stroke-primary" strokeWidth="2" strokeDasharray="5 5" fill="none" opacity="0.6">
        <path d="M110 90 L200 160 L300 80" />
        <path d="M200 160 L120 245 M200 160 L295 235" />
      </g>
      <rect x="160" y="115" width="80" height="95" rx="8" className="fill-surface stroke-border" strokeWidth="2" />
      <g className="fill-primary" opacity="0.35">
        <rect x="172" y="132" width="56" height="6" rx="3" />
        <rect x="172" y="148" width="44" height="6" rx="3" />
        <rect x="172" y="164" width="52" height="6" rx="3" />
        <rect x="172" y="180" width="36" height="6" rx="3" />
      </g>
      {[
        [110, 90],
        [300, 80],
        [120, 245],
        [295, 235],
      ].map(([x, y]) => (
        <g key={`${x}-${y}`}>
          <circle cx={x} cy={y} r="26" className="fill-surface stroke-border" strokeWidth="2" />
          <circle cx={x} cy={y - 6} r="8" className="fill-primary" />
          <path d={`M${x - 13} ${y + 15} a13 10 0 0 1 26 0`} className="fill-primary" />
        </g>
      ))}
    </svg>
  );
}
