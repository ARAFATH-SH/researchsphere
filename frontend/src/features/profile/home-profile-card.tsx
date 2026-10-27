"use client";

import Link from "next/link";

import { Avatar } from "@/components/avatar";

import { Cover } from "./cover";
import { useMyProfile } from "./my-profile";
import { profileStrength } from "./strength";

// The design's home card (design/screens/Home.html, left aside): 10px radius, rs-border
// line and the card shadow.
const card = "overflow-hidden rounded-card border border-rs-border bg-rs-surface shadow-card";

// HomeProfileCard is the left column of /home: the signed-in user's own profile from
// GET /profiles/me (FR-4.5) with their cover image and photo (FR-4.6), the headline and
// institution, and a stat row with the profile strength. Connections and projects get
// their own rows once those features exist.
export function HomeProfileCard() {
  const result = useMyProfile();

  if (!result) return <HomeProfileSkeleton />;
  if (result.status !== "ok") {
    return (
      <div className={`${card} space-y-2 p-4 text-rs-body`}>
        <p className="text-rs-text-muted">We couldn&apos;t load your profile.</p>
        <Link href="/profile" className="font-medium text-rs-accent-strong hover:underline">
          Go to your profile
        </Link>
      </div>
    );
  }

  const profile = result.profile;
  const line = [profile.headline, profile.institution].filter(Boolean).join(" · ");
  const { percent } = profileStrength(profile);
  return (
    <div className={card}>
      <Cover src={profile.coverUrl} className="aspect-[1584/396] overflow-hidden" />
      <div className="relative -mt-[30px] flex flex-col items-center gap-1.5 px-4 pb-4 text-center">
        <Avatar
          name={profile.fullName}
          src={profile.photoUrl}
          size="md"
          decorative
          className="border-[3px] border-rs-surface"
        />
        <Link
          href="/profile"
          className="rounded-rs-xs text-rs-lead font-bold break-words text-rs-text hover:underline focus-visible:shadow-focus focus-visible:outline-none"
        >
          {profile.fullName}
        </Link>
        {line && <p className="text-rs-caption break-words text-rs-text-muted">{line}</p>}
      </div>
      <div className="flex flex-col gap-1.5 border-t border-rs-divider px-4 py-2.5 text-rs-caption">
        <Link
          href="/profile"
          className="flex justify-between text-rs-text hover:underline focus-visible:shadow-focus focus-visible:outline-none"
        >
          Profile strength{" "}
          <strong className="text-rs-accent-strong">{percent}%</strong>
        </Link>
      </div>
    </div>
  );
}

function HomeProfileSkeleton() {
  const bar = "animate-pulse rounded-full bg-rs-neutral";
  return (
    <>
      <p role="status" className="sr-only">
        Loading profile…
      </p>
      <div aria-hidden="true" className={card}>
        <div className="aspect-[1584/396] animate-pulse bg-rs-accent-soft" />
        <div className="-mt-[30px] flex flex-col items-center gap-2 px-4 pb-4">
          <div className="size-16 animate-pulse rounded-full border-[3px] border-rs-surface bg-rs-neutral" />
          <div className={`h-5 w-3/4 ${bar}`} />
          <div className={`h-3 w-full ${bar}`} />
        </div>
      </div>
    </>
  );
}
