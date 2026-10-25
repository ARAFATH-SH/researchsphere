import Link from "next/link";

import type { Profile } from "./api";
import { editHref } from "./edit-button";
import { SectionCard } from "./profile-sections";
import { profileStrength } from "./strength";

// ProfileSidebar is the narrow column: the owner's profile strength, or a placeholder
// for similar researchers on anyone else's profile (AI recommendations, slice 17).
export function ProfileSidebar({
  profile,
  own,
}: {
  profile: Profile;
  own: boolean;
}) {
  return own ? <StrengthCard profile={profile} /> : <SimilarResearchers />;
}

function StrengthCard({ profile }: { profile: Profile }) {
  const { items, percent } = profileStrength(profile);
  return (
    <SectionCard title="Profile strength">
      <div
        role="progressbar"
        aria-label="Profile strength"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
        className="h-2 overflow-hidden rounded-full bg-primary-soft"
      >
        <div
          className="h-full rounded-full bg-primary"
          style={{ width: `${percent}%` }}
        />
      </div>
      <p className="mt-2 text-sm text-text-muted">{percent}% complete</p>
      <ul aria-label="Profile checklist" className="mt-4 space-y-2 text-sm">
        {items.map((item) => (
          <li key={item.label} className="flex items-start gap-2">
            <CheckMark done={item.done} />
            {item.done ? (
              <span className="text-text">
                {item.label}
                <span className="sr-only"> (done)</span>
              </span>
            ) : (
              <Link
                href={editHref(item.section)}
                className="text-secondary hover:underline"
              >
                {item.action}
              </Link>
            )}
          </li>
        ))}
      </ul>
    </SectionCard>
  );
}

function CheckMark({ done }: { done: boolean }) {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={`mt-px size-4 shrink-0 ${done ? "text-primary" : "text-text-muted"}`}
    >
      <circle cx="12" cy="12" r="9" />
      {done && <path d="m8 12.5 2.5 2.5L16 9.5" />}
    </svg>
  );
}

function SimilarResearchers() {
  return (
    <SectionCard title="Similar researchers">
      <p className="text-sm text-text-muted">
        More researchers like this will appear here
      </p>
    </SectionCard>
  );
}
