"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { buttonClass } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useSession } from "@/features/auth/session";

import { getMyProfile, getProfile, type ProfileResult } from "./api";
import { ProfileHeader } from "./profile-header";
import { ProfileSections } from "./profile-sections";
import { ProfileSidebar } from "./profile-sidebar";
import { ProfileSkeleton } from "./profile-skeleton";

// ProfileView shows a researcher's profile (FR-4.5): the signed-in user's own without
// userId, or when userId is their own ID; anyone else's otherwise.
export function ProfileView({ userId }: { userId?: string }) {
  const session = useSession();
  const own = userId === undefined || (session.status === "signedIn" && session.user.id === userId);
  const [attempt, setAttempt] = useState(0);
  // The result remembers which request it answers, so a new userId or a retry shows the
  // skeleton until its own answer arrives.
  const key = `${own ? "me" : userId}#${attempt}`;
  const [result, setResult] = useState<{ key: string; value: ProfileResult } | null>(null);

  useEffect(() => {
    let current = true;
    void (own ? getMyProfile() : getProfile(userId ?? "")).then((value) => {
      if (current) setResult({ key, value });
    });
    return () => {
      current = false;
    };
  }, [own, userId, key]);

  if (!result || result.key !== key) return <ProfileSkeleton />;
  const r = result.value;
  if (r.status === "notFound") return <NotFound />;
  if (r.status === "error") return <LoadFailed onRetry={() => setAttempt((n) => n + 1)} />;

  return (
    <div className="grid gap-6 md:grid-cols-3">
      <div className="space-y-4 md:col-span-2">
        <ProfileHeader profile={r.profile} own={own} />
        <ProfileSections profile={r.profile} own={own} />
      </div>
      <aside aria-label="Profile sidebar" className="space-y-4">
        <ProfileSidebar profile={r.profile} own={own} />
      </aside>
    </div>
  );
}

function NotFound() {
  return (
    <Card className="mx-auto w-full max-w-[400px] space-y-4 p-6 text-center">
      <h1 className="text-2xl font-semibold text-text">Profile not found</h1>
      <p className="text-text-muted">This profile does not exist or is not available.</p>
      <Link href="/home" className={buttonClass({ variant: "primary", full: true })}>
        Go to Home
      </Link>
    </Card>
  );
}

function LoadFailed({ onRetry }: { onRetry: () => void }) {
  return (
    <Card className="mx-auto w-full max-w-[400px] space-y-4 p-6 text-center">
      <h1 className="text-2xl font-semibold text-text">Something went wrong</h1>
      <p className="text-text-muted">We couldn&apos;t load this profile.</p>
      <button type="button" onClick={onRetry} className={buttonClass({ variant: "primary", full: true })}>
        Try again
      </button>
    </Card>
  );
}
