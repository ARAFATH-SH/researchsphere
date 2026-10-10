import { PreviewFrame } from "./preview-frame";

// MatchPreview is a static mock-up of a collaborator recommendation (AI-2.2): a match score
// and up to 3 shared topics. The researcher is fictional.
export function MatchPreview() {
  return (
    <PreviewFrame label="Preview of a collaborator match, with sample data">
      <p className="text-sm font-semibold text-text-muted">Recommended collaborator</p>
      <div className="mt-3 flex items-start gap-3">
        <span
          className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full bg-secondary text-lg font-semibold text-surface"
          aria-hidden="true"
        >
          MO
        </span>
        <div className="min-w-0 flex-1">
          <p className="font-semibold">Dr. Mira Okafor</p>
          <p className="text-sm text-text-muted">Example University, Department of Biology</p>
        </div>
        <p className="shrink-0 text-right">
          <span className="block text-2xl font-semibold text-primary">87%</span>
          <span className="text-xs text-text-muted">match</span>
        </p>
      </div>
      <p className="mt-4 text-sm text-text-muted">Topics you share</p>
      <ul className="mt-2 flex flex-wrap gap-2">
        {["Protein folding", "Machine learning", "Bioinformatics"].map((topic) => (
          <li key={topic} className="rounded-full bg-primary-soft px-3 py-1 text-sm text-primary">
            {topic}
          </li>
        ))}
      </ul>
      {/* A picture of a button: not focusable and not announced as one. */}
      <span
        className="mt-5 flex w-full items-center justify-center rounded-full bg-primary px-4 py-2 text-sm font-semibold text-surface"
        aria-hidden="true"
      >
        Connect
      </span>
    </PreviewFrame>
  );
}
