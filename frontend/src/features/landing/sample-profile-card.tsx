import Image from "next/image";

// SampleProfileCard is the hero's static profile card (design/screens/Landing.html): a cover
// strip, a 65px avatar, name, headline, interest chips and a match line with Connect. The
// researcher is fictional and the card carries a "Sample data" badge (docs/ui-guidelines.md,
// Product previews); screen readers get the figure's label instead of the parts.
const interests = ["Medical imaging", "Explainable AI", "PyTorch"];

export function SampleProfileCard({ className = "" }: { className?: string }) {
  return (
    <figure
      aria-label="Sample profile card of a fictional researcher, with sample data"
      className={`m-0 min-w-0 overflow-hidden rounded-card-lg border border-rs-border bg-rs-surface shadow-card ${className}`}
    >
      <div className="relative aspect-[1584/396] overflow-hidden bg-rs-accent-strong">
        <Image src="/images/landing/sample-cover.svg" alt="" fill sizes="(min-width: 768px) 50vw, 100vw" className="object-cover" />
        <span
          aria-hidden="true"
          className="absolute top-2 right-2 rounded-rs-pill bg-rs-surface px-2 py-0.5 text-rs-caption font-medium text-rs-heading"
        >
          Sample data
        </span>
      </div>
      <div className="-mt-8 flex flex-col gap-3 px-5 pb-5" aria-hidden="true">
        <span className="relative flex size-[65px] items-center justify-center rounded-full border-[3px] border-rs-surface bg-rs-accent-deep text-[22px] font-bold text-rs-surface">
          NR
        </span>
        <div>
          <p className="text-rs-lead font-medium">Nadia Rahman</p>
          <p className="text-rs-text-muted">PhD student · Computer Vision</p>
        </div>
        <ul className="flex flex-wrap gap-1.5">
          {interests.map((interest) => (
            <li key={interest} className="rounded-rs-pill bg-rs-accent-soft px-2.5 py-1 text-rs-caption text-rs-heading">
              {interest}
            </li>
          ))}
        </ul>
        <div className="flex items-center justify-between gap-2 border-t border-rs-divider pt-3">
          <span>87% research match with you</span>
          {/* A picture of a button: not focusable and not announced as one. */}
          <span className="rounded-rs-pill bg-rs-accent-strong px-3.5 py-1.5 font-medium text-rs-surface">Connect</span>
        </div>
      </div>
    </figure>
  );
}
