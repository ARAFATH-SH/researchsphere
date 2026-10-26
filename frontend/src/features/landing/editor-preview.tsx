import { PreviewFrame } from "./preview-frame";

// EditorPreview is a static mock-up of the shared LaTeX editor (FR-12, FR-13.1): source on the
// left, the compiled PDF on the right, and two co-authors' cursors in distinct colours
// (FR-12.5). The co-authors are fictional.
export function EditorPreview() {
  return (
    <PreviewFrame label="Preview of the shared LaTeX editor, with sample data">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div className="overflow-hidden rounded-lg border border-rs-border bg-rs-subtle p-3 font-mono text-xs leading-6 text-rs-text">
          <p className="mb-1 font-sans text-xs font-medium text-rs-text-muted">main.tex</p>
          <p>
            <span className="text-rs-info">\section</span>{"{Results}"}
          </p>
          <p className="relative">
            Our model predicts
            <Cursor name="Sam" tone="primary" />
          </p>
          <p>folding rates within</p>
          <p className="relative">
            <span className="text-rs-info">\cite</span>
            {"{okafor2026}"}.
            <Cursor name="Ana" tone="secondary" />
          </p>
          <p>
            <span className="text-rs-info">\begin</span>
            {"{figure}"}
          </p>
        </div>
        <div className="rounded-lg border border-rs-border bg-rs-surface p-3" aria-hidden="true">
          <p className="mb-2 text-xs font-medium text-rs-text-muted">main.pdf</p>
          <div className="space-y-2">
            <div className="h-2.5 w-1/2 rounded-full bg-rs-text" />
            <div className="h-2 w-full rounded-full bg-rs-neutral" />
            <div className="h-2 w-11/12 rounded-full bg-rs-neutral" />
            <div className="h-2 w-4/5 rounded-full bg-rs-neutral" />
            <div className="mt-3 h-14 w-full rounded bg-rs-accent-soft" />
            <div className="h-2 w-full rounded-full bg-rs-neutral" />
            <div className="h-2 w-2/3 rounded-full bg-rs-neutral" />
          </div>
        </div>
      </div>
    </PreviewFrame>
  );
}

const tones = {
  primary: { bar: "bg-rs-accent-strong", tag: "bg-rs-accent-strong" },
  secondary: { bar: "bg-rs-info", tag: "bg-rs-info" },
};

// Cursor is a co-author's caret with a name tag beside it; it sits at the end of a line.
function Cursor({ name, tone }: { name: string; tone: keyof typeof tones }) {
  return (
    <span className="relative ml-0.5 inline-block h-4 w-0.5 align-middle">
      <span className={`absolute inset-0 ${tones[tone].bar}`} />
      <span
        className={`absolute top-1/2 left-1.5 -translate-y-1/2 rounded px-1 font-sans text-[10px] leading-4 font-medium whitespace-nowrap text-rs-surface ${tones[tone].tag}`}
      >
        {name}
      </span>
    </span>
  );
}
