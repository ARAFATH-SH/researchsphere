"use client";

import Link from "next/link";

import { rsButtonClass } from "@/components/ui/button";
import { editHref } from "@/features/profile/edit-button";
import { useMyProfile } from "@/features/profile/my-profile";

import { emptyCard, homeCard } from "./cards";

const heading = "text-rs-lead leading-rs-snug font-medium";

// Recommendations is the top of the home page's right column. Without research interests
// there is nothing to match on, so it asks for some and links to the interests dialog.
// With interests it holds the place of the recommended collaborators (AI-2), which a later
// slice builds. Nothing shows while the profile loads or when it fails to load.
export function Recommendations() {
  const result = useMyProfile();
  if (result?.status !== "ok") return null;

  if (result.profile.interests.length === 0) {
    return (
      <section aria-labelledby="recommendations-heading" className={`${emptyCard} flex flex-col gap-2 p-4`}>
        <h2 id="recommendations-heading" className={heading}>
          Get better recommendations
        </h2>
        <p className="text-rs-body text-rs-text">
          Add research interests, skills or a short bio and we’ll suggest collaborators and projects — no
          publications needed.
        </p>
        <Link href={editHref("interests")} className={`${rsButtonClass({ size: "sm" })} self-start`}>
          Add interests
        </Link>
      </section>
    );
  }

  return (
    <section aria-labelledby="recommendations-heading" className={`${homeCard} flex flex-col gap-3 p-4`}>
      <h2 id="recommendations-heading" className={heading}>
        Recommended collaborators
      </h2>
      <p className="text-rs-body text-rs-text-muted">Recommendations are coming soon.</p>
    </section>
  );
}
