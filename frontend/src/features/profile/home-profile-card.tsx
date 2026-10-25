"use client";

import Link from "next/link";

import { Avatar } from "@/components/avatar";
import { Card } from "@/components/ui/card";

import { Cover } from "./cover";
import { affiliation, locationLine } from "./format";
import { useMyProfile } from "./my-profile";
import { profileStrength } from "./strength";

// HomeProfileCard is the left column of /home: the signed-in user's own profile from
// GET /profiles/me (FR-4.5) with their cover image and photo (FR-4.6), a link to the
// profile and its strength.
export function HomeProfileCard() {
  const result = useMyProfile();

  if (!result) return <HomeProfileSkeleton />;
  if (result.status !== "ok") {
    return (
      <Card className="space-y-2 p-4 text-sm">
        <p className="text-text-muted">We couldn&apos;t load your profile.</p>
        <Link
          href="/profile"
          className="font-semibold text-secondary hover:underline"
        >
          Go to your profile
        </Link>
      </Card>
    );
  }

  const profile = result.profile;
  const works = affiliation(profile);
  const location = locationLine(profile);
  const { percent } = profileStrength(profile);
  return (
    <Card className="overflow-hidden">
      <Cover src={profile.coverUrl} className="h-14" />
      <div className="px-4 pb-4">
        <Link
          href="/profile"
          className="group block rounded-lg focus-visible:ring-2 focus-visible:ring-primary focus-visible:outline-none"
        >
          <Avatar
            name={profile.fullName}
            src={profile.photoUrl}
            size="md"
            decorative
            className="relative -mt-8 border-4 border-surface"
          />
          <span className="mt-2 block text-lg font-semibold break-words text-text group-hover:underline">
            {profile.fullName}
          </span>
        </Link>
        {profile.headline && (
          <p className="mt-1 text-sm break-words text-text">
            {profile.headline}
          </p>
        )}
        {works && <p className="mt-1 text-sm text-text-muted">{works}</p>}
        {location && <p className="text-sm text-text-muted">{location}</p>}
      </div>
      <Link
        href="/profile"
        className="block border-t border-border px-4 py-3 text-sm text-text-muted hover:bg-background hover:text-text focus-visible:ring-2 focus-visible:ring-primary focus-visible:outline-none focus-visible:ring-inset"
      >
        Profile strength:{" "}
        <span className="font-semibold text-primary">{percent}%</span>
      </Link>
    </Card>
  );
}

function HomeProfileSkeleton() {
  const bar = "animate-pulse rounded-full bg-border";
  return (
    <>
      <p role="status" className="sr-only">
        Loading profile…
      </p>
      <Card aria-hidden="true" className="overflow-hidden">
        <div className="h-14 animate-pulse bg-primary-soft" />
        <div className="space-y-2 px-4 pb-4">
          <div className="-mt-8 size-16 animate-pulse rounded-full border-4 border-surface bg-border" />
          <div className={`h-5 w-3/4 ${bar}`} />
          <div className={`h-4 w-full ${bar}`} />
          <div className={`h-4 w-1/2 ${bar}`} />
        </div>
      </Card>
    </>
  );
}
