"use client";

import { type RefObject, useEffect, useId, useRef, useState } from "react";

import { Container } from "@/components/Container";
import { buttonClass } from "@/components/ui/button";
import { Card } from "@/components/ui/card";

import type { Profile } from "./api";
import { EditButton, editButtonId } from "./edit-button";
import { affiliation, countryName, initials } from "./format";
import { MoreMenu } from "./more-menu";
import type { EditControls } from "./profile-sections";

type ProfileHeaderProps = {
  profile: Profile;
  edit?: EditControls;
  // onContact opens the contact info dialog; without it the link is not shown.
  onContact?: () => void;
};

// topBarHeight is the height of the fixed top bar (h-14), under which the sticky bar sits.
const topBarHeight = 56;

// ProfileHeader is the top card: a cover band, the avatar, the name, where the researcher
// works and a "Contact info" link (FR-4.7). On wide screens the current institution and
// the latest education entry sit on the right. The owner gets a pencil that opens the
// intro dialog; anyone else a Connect button that waits for connections (slice 13). Once
// the card scrolls out of view, a slim bar under the top bar repeats the name and buttons.
export function ProfileHeader({ profile, edit, onContact }: ProfileHeaderProps) {
  const works = affiliation(profile);
  const card = useRef<HTMLDivElement>(null);
  const pinned = useScrolledPast(card);
  return (
    <>
      <Card ref={card} className="overflow-hidden">
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
            <div className="flex items-center gap-1 pt-4">
              <HeaderActions profile={profile} edit={edit} withIds />
            </div>
          </div>
          <div className="flex gap-6">
            <div className="min-w-0 flex-1">
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
            <Affiliations profile={profile} />
          </div>
        </div>
      </Card>
      {pinned && <StickyBar profile={profile} edit={edit} />}
    </>
  );
}

// HeaderActions are the header's buttons: the pencil or Connect, then More. Only the
// header card's own pencil carries the id that edit dialogs give the focus back to.
function HeaderActions({ profile, edit, withIds = false }: Pick<ProfileHeaderProps, "profile" | "edit"> & { withIds?: boolean }) {
  return (
    <>
      {edit ? (
        <EditButton id={withIds ? editButtonId("intro") : undefined} label="Edit intro" onClick={() => edit.open("intro")} />
      ) : (
        <ConnectButton />
      )}
      <MoreMenu userId={profile.userId} />
    </>
  );
}

// Affiliations lists the current institution and the latest education entry, each with a
// square initial badge. The API returns education newest first. Below 768px they are left
// out: the institution is already in the text beside them.
function Affiliations({ profile }: { profile: Profile }) {
  const items = [profile.institution, profile.education[0]?.institution].filter((name): name is string => Boolean(name));
  if (items.length === 0) return null;
  return (
    <ul aria-label="Affiliations" className="mt-4 hidden w-56 shrink-0 space-y-3 md:block">
      {items.map((name, i) => (
        <li key={i} className="flex items-center gap-2 text-sm font-semibold text-text">
          <span
            aria-hidden="true"
            className="flex size-8 shrink-0 items-center justify-center rounded-md bg-primary-soft text-sm text-primary"
          >
            {initials(name).slice(0, 1)}
          </span>
          <span className="min-w-0 break-words">{name}</span>
        </li>
      ))}
    </ul>
  );
}

// useScrolledPast is true once the element has scrolled up behind the top bar. Without
// IntersectionObserver it stays false.
function useScrolledPast(target: RefObject<HTMLElement | null>): boolean {
  const [past, setPast] = useState(false);
  useEffect(() => {
    const el = target.current;
    if (!el || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      ([entry]) => setPast(!entry.isIntersecting && entry.boundingClientRect.top < topBarHeight),
      { rootMargin: `-${topBarHeight}px 0px 0px 0px` },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [target]);
  return past;
}

// StickyBar is the slim bar under the top bar while the header card is out of view.
function StickyBar({ profile, edit }: Pick<ProfileHeaderProps, "profile" | "edit">) {
  return (
    <div
      role="region"
      aria-label="Profile summary"
      className="fixed inset-x-0 top-14 z-10 border-b border-border bg-surface shadow-sm"
    >
      <Container size="app" className="flex h-14 items-center gap-3">
        <div
          aria-hidden="true"
          className="flex size-9 shrink-0 items-center justify-center rounded-full bg-primary-soft text-sm font-semibold text-primary"
        >
          {initials(profile.fullName)}
        </div>
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-semibold text-text">{profile.fullName}</p>
          {profile.headline && <p className="truncate text-xs text-text-muted">{profile.headline}</p>}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <HeaderActions profile={profile} edit={edit} />
        </div>
      </Container>
    </div>
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
