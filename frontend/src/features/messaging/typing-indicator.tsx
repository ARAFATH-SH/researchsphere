// TypingIndicator is the design's three dots and "<name> is typing…".
export function TypingIndicator({ name }: { name: string }) {
  return (
    <span className="flex min-w-0 items-center gap-2 text-rs-caption text-rs-text-muted">
      <span aria-hidden="true" className="flex flex-none gap-[3px]">
        {[0, 1, 2].map((dot) => (
          <span key={dot} className="size-1.5 rounded-full bg-rs-text-muted" />
        ))}
      </span>
      <span className="truncate">{name} is typing…</span>
    </span>
  );
}
