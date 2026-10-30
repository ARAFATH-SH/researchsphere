import type { ReactNode } from "react";

import { ClampedText } from "./profile-sections";
import type { Publication } from "./publications-api";

// venueYear is the "Venue · Year" line; either part may be missing (an imported record can
// have no year), and with neither there is no line.
export function venueYear(p: { venue: string | null; year: number | null }): string {
  return [p.venue, p.year].filter((v) => v !== null && v !== "").join(" · ");
}

// doiUrl is the doi.org link for a bare DOI.
export function doiUrl(doi: string): string {
  return `https://doi.org/${doi}`;
}

const linkClass =
  "self-start rounded-rs-2xs text-rs-caption break-all text-rs-accent-strong hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none";

// PublicationItem is one publication as in design/screens/Profile.html: a document tile,
// the bold title, "Venue · Year", the DOI as a doi.org link (or the record's own link when
// it has no DOI) and the abstract clamped to three lines. actions, such as the owner's
// menu, sit at the end of the row.
export function PublicationItem({
  publication: p,
  actions,
}: {
  publication: Publication;
  actions?: ReactNode;
}) {
  const meta = venueYear(p);
  return (
    <li className="flex items-start gap-3 border-t border-rs-divider pt-3 first:border-t-0 first:pt-0">
      <span
        aria-hidden="true"
        className="flex size-10 shrink-0 items-center justify-center rounded-control bg-rs-accent-soft text-rs-accent-strong"
      >
        <DocumentIcon />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5 text-rs-body">
        <p className="font-bold break-words text-rs-text">{p.title}</p>
        {meta && <p className="text-rs-text">{meta}</p>}
        {p.doi ? (
          <a
            href={doiUrl(p.doi)}
            target="_blank"
            rel="noopener"
            className={linkClass}
          >
            doi:{p.doi}
          </a>
        ) : (
          p.url && (
            <a href={p.url} target="_blank" rel="noopener" className={linkClass}>
              View publication
            </a>
          )
        )}
        {p.abstract && <ClampedText text={p.abstract} />}
      </div>
      {actions}
    </li>
  );
}

function DocumentIcon() {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinejoin="round"
      className="size-5"
    >
      <path d="M6 3h9l4 4v14H6z" />
      <path d="M14 3v5h5M9 13h7M9 17h7" />
    </svg>
  );
}
