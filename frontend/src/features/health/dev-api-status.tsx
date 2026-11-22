import { ApiStatus } from "./api-status";

// DevApiStatus shows the API status while developing. The design has no API status, so it
// renders nothing unless NODE_ENV is "development". By default it is fixed in the bottom left
// corner and hides at 640px and narrower, where the bottom bars sit. A page whose own content
// runs to that corner (the workspace's file tree) shows it inline instead; while an inline one
// is on the page the fixed one hides (CSS :has, so no hook reads the URL in the root layout).
export function DevApiStatus({ inline = false }: { inline?: boolean }) {
  if (process.env.NODE_ENV !== "development") return null;
  if (inline) {
    return (
      <div data-testid="dev-api-status" data-dev-api-inline="" className="text-rs-caption text-rs-text-muted">
        <ApiStatus />
      </div>
    );
  }
  return (
    <div
      data-testid="dev-api-status"
      className="pointer-events-none fixed bottom-2 left-2 z-[80] rounded-rs-pill border border-rs-border bg-rs-surface px-2.5 py-0.5 text-rs-text-muted shadow-popover max-[640px]:hidden [body:has([data-dev-api-inline])_&]:hidden"
    >
      <ApiStatus />
    </div>
  );
}
