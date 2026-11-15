import Link from "next/link";

import { RecommendationList } from "@/features/recommendation/recommendation-list";

import type { Profile } from "./api";
import { ContactCard } from "./contact-card";
import { editHref } from "./edit-button";
import { SectionCard } from "./profile-sections";
import { profileStrength } from "./strength";

// ProfileSidebar is the aside of design/screens/Profile.html: the contact info card, then,
// on the owner's profile, the profile strength, then on every profile "Connect with
// researchers": the viewer's top 3 recommendations (AI-2), hidden when they have none. The
// design's "People also viewed" is not built. onEditContact, given on the owner's profile,
// opens the contact dialog.
export function ProfileSidebar({
  profile,
  onEditContact,
}: {
  profile: Profile;
  onEditContact?: () => void;
}) {
  return (
    <>
      <ContactCard contact={profile.contact} onEdit={onEditContact} />
      {onEditContact && <StrengthCard profile={profile} />}
      <RecommendationList limit={3} variant="profile" />
    </>
  );
}

function StrengthCard({ profile }: { profile: Profile }) {
  const { items, percent } = profileStrength(profile);
  return (
    <SectionCard title="Profile strength" aside>
      <div
        role="progressbar"
        aria-label="Profile strength"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
        className="h-1.5 overflow-hidden rounded-full bg-rs-neutral"
      >
        <div
          className="h-full rounded-full bg-rs-accent-strong"
          style={{ width: `${percent}%` }}
        />
      </div>
      <p className="text-rs-caption text-rs-text-muted">{percent}% complete</p>
      <ul aria-label="Profile checklist" className="space-y-2 text-rs-body">
        {items.map((item) => (
          <li key={item.label} className="flex items-start gap-2">
            <CheckMark done={item.done} />
            {item.done ? (
              <span className="text-rs-text">
                {item.label}
                <span className="sr-only"> (done)</span>
              </span>
            ) : (
              <Link
                href={editHref(item.section)}
                className="text-rs-accent-strong hover:text-rs-accent-hover hover:underline"
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
      className={`mt-px size-4 shrink-0 ${done ? "text-rs-accent-strong" : "text-rs-text-muted"}`}
    >
      <circle cx="12" cy="12" r="9" />
      {done && <path d="m8 12.5 2.5 2.5L16 9.5" />}
    </svg>
  );
}
