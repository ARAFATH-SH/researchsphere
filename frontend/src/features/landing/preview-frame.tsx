import type { ReactNode } from "react";

import { Card } from "@/components/ui/card";

// PreviewFrame is the card a static product mock-up sits in. The "Sample data" badge makes
// clear that the names and numbers are made up; screen readers get the label instead.
export function PreviewFrame({ label, children }: { label: string; children: ReactNode }) {
  return (
    <figure aria-label={label} className="w-full">
      <Card className="overflow-hidden">
        <div className="flex items-center justify-between gap-2 border-b border-rs-border bg-rs-subtle px-4 py-2">
          <span className="flex gap-1.5" aria-hidden="true">
            <span className="h-2.5 w-2.5 rounded-full bg-rs-neutral" />
            <span className="h-2.5 w-2.5 rounded-full bg-rs-neutral" />
            <span className="h-2.5 w-2.5 rounded-full bg-rs-neutral" />
          </span>
          <span className="rounded-full bg-rs-accent-soft px-2 py-0.5 text-xs font-medium text-rs-accent-strong">
            Sample data
          </span>
        </div>
        <div className="p-4 sm:p-5">{children}</div>
      </Card>
    </figure>
  );
}
