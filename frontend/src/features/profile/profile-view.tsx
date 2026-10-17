"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";

import { buttonClass } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useSession } from "@/features/auth/session";

import { AboutDialog } from "./about-dialog";
import { getMyProfile, getProfile, type Profile, type ProfileResult } from "./api";
import { editButtonId, type EditSection, isEditSection } from "./edit-button";
import { EducationDialog } from "./education-dialog";
import { IntroDialog } from "./intro-dialog";
import { ProfileHeader } from "./profile-header";
import { type EditControls, ProfileSections } from "./profile-sections";
import { ProfileSidebar } from "./profile-sidebar";
import { ProfileSkeleton } from "./profile-skeleton";
import { TagsDialog } from "./tags-dialog";
import { Toast, toastDuration } from "./toast";

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

  // reload fetches the own profile again after an edit, keeping the page on screen.
  const reload = useCallback(() => {
    void getMyProfile().then((value) => {
      if (value.status === "ok") setResult({ key, value });
    });
  }, [key]);

  if (!result || result.key !== key) return <ProfileSkeleton />;
  const r = result.value;
  if (r.status === "notFound") return <NotFound />;
  if (r.status === "error") return <LoadFailed onRetry={() => setAttempt((n) => n + 1)} />;

  if (!own) {
    return (
      <div className="grid gap-6 md:grid-cols-3">
        <div className="space-y-4 md:col-span-2">
          <ProfileHeader profile={r.profile} />
          <ProfileSections profile={r.profile} />
        </div>
        <aside aria-label="Profile sidebar" className="space-y-4">
          <ProfileSidebar profile={r.profile} own={false} />
        </aside>
      </div>
    );
  }
  return <OwnProfile profile={r.profile} reload={reload} />;
}

// OwnProfile is the owner's profile with its edit dialogs (FR-4.1 to FR-4.4). A pencil
// opens a section's dialog, and so does /profile?edit=<section>, which the profile
// checklist and the empty-section prompts link to.
function OwnProfile({ profile, reload }: { profile: Profile; reload: () => void }) {
  const router = useRouter();
  const params = useSearchParams();
  const requested = params.get("edit");
  const [editing, setEditing] = useState<EditSection | null>(null);
  // handled is the ?edit= value whose dialog was already shown, so closing it does not
  // open it again before the URL changes.
  const [handled, setHandled] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const toastTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(toastTimer.current), []);
  if (requested === null && handled !== null) setHandled(null);
  const open = editing ?? (isEditSection(requested) && handled !== requested ? requested : null);

  function close() {
    setEditing(null);
    if (requested !== null) {
      setHandled(requested);
      router.replace("/profile", { scroll: false });
    }
  }

  function saved(message = "Saved") {
    close();
    reload();
    setToast(message);
    clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => setToast(null), toastDuration);
  }

  const edit: EditControls = { open: setEditing };
  const common = { profile, onClose: close, returnFocusId: open ? editButtonId(open) : undefined };
  return (
    <>
      <div className="grid gap-6 md:grid-cols-3">
        <div className="space-y-4 md:col-span-2">
          <ProfileHeader profile={profile} edit={edit} />
          <ProfileSections profile={profile} edit={edit} />
        </div>
        <aside aria-label="Profile sidebar" className="space-y-4">
          <ProfileSidebar profile={profile} own />
        </aside>
      </div>
      {open === "intro" && <IntroDialog {...common} onSaved={() => saved()} />}
      {open === "about" && <AboutDialog {...common} onSaved={() => saved()} />}
      {(open === "interests" || open === "skills") && <TagsDialog key={open} kind={open} {...common} onChanged={reload} />}
      {open === "education" && <EducationDialog {...common} onSaved={saved} />}
      {toast && <Toast message={toast} />}
    </>
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
