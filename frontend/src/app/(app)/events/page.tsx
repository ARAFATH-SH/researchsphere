import { Suspense } from "react";

import { EventsPage } from "@/features/event/events-page";

// Conferences, workshops and journal calls (FR-14.1 to FR-14.3).
export default function Page() {
  return (
    <Suspense
      fallback={
        <p role="status" className="text-rs-body text-rs-text-muted">
          Loading venues…
        </p>
      }
    >
      <EventsPage />
    </Suspense>
  );
}
