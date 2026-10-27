import { PreviewFrame } from "./preview-frame";

// VenuePreview is a static mock-up of venue recommendations (AI-6.2): each venue with its
// scope similarity and submission deadline, and the note AI-6.4 requires. Venues are fictional.
const venues = [
  { name: "Example Conference on Computational Biology", type: "Conference", fit: 92, deadline: "15 Jan 2027" },
  { name: "Journal of Sample Bioinformatics", type: "Journal", fit: 85, deadline: "1 Feb 2027" },
  { name: "Workshop on Imaginary Proteins", type: "Workshop", fit: 78, deadline: "20 Feb 2027" },
];

export function VenuePreview() {
  return (
    <PreviewFrame label="Preview of venue recommendations, with sample data">
      <p className="text-sm font-medium text-rs-text-muted">Venues that fit your abstract</p>
      <ul className="mt-3 divide-y divide-rs-border">
        {venues.map((venue) => (
          <li key={venue.name} className="flex items-center gap-3 py-3">
            <div className="min-w-0 flex-1">
              <p className="font-medium">{venue.name}</p>
              <p className="text-sm text-rs-text-muted">
                {venue.type} · Deadline {venue.deadline}
              </p>
            </div>
            <p className="shrink-0 rounded-full bg-rs-accent-soft px-3 py-1 text-sm font-medium text-rs-accent-strong">
              {venue.fit}% fit
            </p>
          </li>
        ))}
      </ul>
      <p className="mt-2 text-xs text-rs-text-muted">A scope match says nothing about a venue&apos;s quality.</p>
    </PreviewFrame>
  );
}
