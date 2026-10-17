"use client";

import { useId } from "react";

import { buttonClass } from "@/components/ui/button";
import { Card } from "@/components/ui/card";

import type { Profile } from "./api";
import { EditButton, editButtonId } from "./edit-button";
import { affiliation, countryName, initials } from "./format";
import type { EditControls } from "./profile-sections";

type ProfileHeaderProps = {
  profile: Profile;
  edit?: EditControls;
  // onContact opens the contact info dialog; without it the link is not shown.
  onContact?: () => void;
};

// ProfileHeader is the top card: a cover band, the avatar, the name, where the researcher
// works and a "Contact info" link (FR-4.7). The owner gets a pencil that opens the intro
// dialog; anyone else a Connect button that waits for connections (slice 13).
export function ProfileHeader({ profile, edit, onContact }: ProfileHeaderProps) {
  const works = affiliation(profile);
  return (
    <Card className="overflow-hidden">
      <div aria-hidden="true" className="h-24 bg-linear-to-r from-primary to-secondary md:h-32" />
      <div className="px-4 pb-6 md:px-6">
        <div className="flex items-end justify-between gap-4">
          <div
            role="img"
            aria-label={profile.fullName}
            className="-mt-12 flex size-24 shrink-0 items-center justify-center rounded-full border-4 border-surface bg-primary-soft text-3xl font-semibold text-primary md:-mt-16 md:size-32 md:text-4xl"
          >
            {initials(profile.fullName)}
          </div>
          <div className="pt-4">
            {edit ? (
              <EditButton id={editButtonId("intro")} label="Edit intro" onClick={() => edit.open("intro")} />
            ) : (
              <ConnectButton />
            )}
          </div>
        </div>
        <h1 className="mt-4 text-2xl font-semibold break-words text-text md:text-3xl">{profile.fullName}</h1>
        {profile.headline && <p className="mt-1 text-base text-text">{profile.headline}</p>}
        <div className="mt-2 space-y-0.5 text-sm text-text-muted">
          {works && <p>{works}</p>}
          {profile.department && <p>{profile.department}</p>}
          {profile.country && <p>{countryName(profile.country)}</p>}
        </div>
        {onContact && (
          <button
            id={editButtonId("contact")}
            type="button"
            onClick={onContact}
            className="mt-2 rounded-sm text-sm font-semibold text-secondary hover:underline focus-visible:ring-2 focus-visible:ring-primary focus-visible:outline-none"
          >
            Contact info
          </button>
        )}
      </div>
    </Card>
  );
}

// ConnectButton is shown but does nothing until connections exist. It uses aria-disabled
// rather than disabled, so keyboard users can still reach it and hear the tooltip.
function ConnectButton() {
  const tipId = useId();
  return (
    <span className="group relative inline-flex">
      <button
        type="button"
        aria-disabled="true"
        aria-describedby={tipId}
        onClick={(e) => e.preventDefault()}
        className={`${buttonClass({ variant: "primary", size: "md" })} cursor-not-allowed opacity-60 hover:bg-primary`}
      >
        Connect
      </button>
      <span
        id={tipId}
        role="tooltip"
        className="pointer-events-none absolute top-full right-0 z-10 mt-2 hidden rounded-md bg-text px-2 py-1 text-xs whitespace-nowrap text-surface group-focus-within:block group-hover:block"
      >
        Available soon
      </span>
    </span>
  );
}
