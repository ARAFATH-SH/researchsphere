"use client";

import { type KeyboardEvent, type PointerEvent, type ReactNode, useRef, useState } from "react";

const min = 30;
const max = 75;
const step = 5;

// Split puts the editor and the PDF side by side (FR-13.1) with a divider the member drags, or
// moves with the arrow keys, to share the width; the editor keeps 30 to 75 percent.
export function Split({ left, right, initial = 58 }: { left: ReactNode; right: ReactNode; initial?: number }) {
  const [share, setShare] = useState(initial);
  const box = useRef<HTMLDivElement>(null);
  const dragging = useRef(false);
  const clamp = (n: number) => Math.min(max, Math.max(min, Math.round(n)));

  function onPointerMove(e: PointerEvent<HTMLDivElement>) {
    if (!dragging.current || !box.current) return;
    const rect = box.current.getBoundingClientRect();
    if (rect.width > 0) setShare(clamp(((e.clientX - rect.left) / rect.width) * 100));
  }

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    const moves: Record<string, number> = { ArrowLeft: share - step, ArrowRight: share + step, Home: min, End: max };
    if (!(e.key in moves)) return;
    e.preventDefault();
    setShare(clamp(moves[e.key]));
  }

  return (
    <div ref={box} className="flex min-h-0 min-w-0 flex-1">
      <div className="flex min-h-0 min-w-0 flex-col" style={{ flexBasis: `${share}%` }}>
        {left}
      </div>
      <div
        role="separator"
        tabIndex={0}
        aria-orientation="vertical"
        aria-label="Resize the editor and the PDF"
        aria-valuemin={min}
        aria-valuemax={max}
        aria-valuenow={share}
        onKeyDown={onKeyDown}
        onPointerDown={(e) => {
          dragging.current = true;
          e.currentTarget.setPointerCapture?.(e.pointerId);
        }}
        onPointerMove={onPointerMove}
        onPointerUp={() => (dragging.current = false)}
        onPointerCancel={() => (dragging.current = false)}
        className="w-1.5 flex-none cursor-col-resize touch-none border-x border-rs-border bg-rs-surface hover:bg-rs-accent-soft focus-visible:bg-rs-accent-soft focus-visible:outline-none"
      />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">{right}</div>
    </div>
  );
}
