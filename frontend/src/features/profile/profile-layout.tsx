import type { ReactNode } from "react";

// ProfileLayout is the wrapping row of design/screens/Profile.html: the main column and the
// aside share it by their flex weights, and the aside drops below once the row is narrower
// than both. Under 640px the gap shrinks and the aside takes the full width.
export function ProfileLayout({
  main,
  aside,
}: {
  main: ReactNode;
  aside: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-start gap-6 max-[640px]:gap-4">
      <div className="flex min-w-0 flex-[999_1_560px] flex-col gap-3">
        {main}
      </div>
      <aside
        aria-label="Profile sidebar"
        className="flex max-w-[320px] min-w-0 flex-[1_1_280px] flex-col gap-3 max-[640px]:max-w-none"
      >
        {aside}
      </aside>
    </div>
  );
}
