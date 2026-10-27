import { ApiStatus } from "./api-status";

// DevApiStatus shows the API status in the bottom left corner while developing. The design
// has no API status, so it renders nothing unless NODE_ENV is "development", and it hides at
// 640px and narrower, where the bottom bars sit.
export function DevApiStatus() {
  if (process.env.NODE_ENV !== "development") return null;
  return (
    <div className="pointer-events-none fixed bottom-2 left-2 z-[80] rounded-rs-pill border border-rs-border bg-rs-surface px-2.5 py-0.5 text-rs-text-muted shadow-popover max-[640px]:hidden">
      <ApiStatus />
    </div>
  );
}
