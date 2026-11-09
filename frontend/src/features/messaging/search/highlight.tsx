import type { TextRange } from "../api";

// Piece is a stretch of a snippet, marked or not.
export type Piece = { text: string; marked: boolean };

// pieces splits a snippet by its highlight ranges (FR-16.8). The ranges count Unicode code
// points, so the snippet is indexed as Array.from(snippet), never as a string: a range never
// splits an emoji or a character outside the Basic Multilingual Plane. Ranges that overlap,
// are out of order or run past the end are clipped, never thrown on.
export function pieces(snippet: string, ranges: TextRange[]): Piece[] {
  const chars = Array.from(snippet);
  const out: Piece[] = [];
  let at = 0;
  const push = (end: number, marked: boolean) => {
    if (end <= at) return;
    out.push({ text: chars.slice(at, end).join(""), marked });
    at = end;
  };
  for (const r of [...ranges].sort((a, b) => a.start - b.start)) {
    const start = Math.max(at, Math.min(r.start, chars.length));
    const end = Math.min(chars.length, r.start + r.length);
    push(start, false);
    push(end, true);
  }
  push(chars.length, false);
  return out;
}

// Highlighted is a snippet with its matches in <mark>. It renders text only, never HTML.
export function Highlighted({ snippet, ranges }: { snippet: string; ranges: TextRange[] }) {
  return (
    <>
      {pieces(snippet, ranges).map((p, i) =>
        p.marked ? (
          <mark key={i} className="rounded-rs-2xs bg-rs-accent-soft px-0.5 font-bold text-rs-accent-deep">
            {p.text}
          </mark>
        ) : (
          <span key={i}>{p.text}</span>
        ),
      )}
    </>
  );
}
