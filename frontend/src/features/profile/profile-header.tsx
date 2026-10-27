"use client";

import { type RefObject, useEffect, useRef, useState } from "react";

import { Avatar } from "@/components/avatar";
import { Container } from "@/components/Container";
import { homeCard } from "@/features/home/cards";

import type { Profile } from "./api";
import { Cover } from "./cover";
import { CameraIcon, EditButton, editButtonId } from "./edit-button";
import { locationLine } from "./format";
import { MoreMenu } from "./more-menu";
import type { EditControls } from "./profile-sections";

type ProfileHeaderProps = {
  profile: Profile;
  edit?: EditControls;
  // onContact opens the contact info dialog; without it the link is not shown.
  onContact?: () => void;
};

// topBarHeight is the height of the sticky top bar (52px), under which the sticky profile
// bar sits. At 480px and narrower the phone top bar is 56px high.
const topBarHeight = 52;

// ProfileHeader is the profile summary card of design/screens/Profile.html: the cover image
// and photo (FR-4.6), the name, the headline, one line with the job, institution and place
// and a "Contact info" link (FR-4.7), then the action row. The owner gets a camera button on
// the cover, a clickable photo and a pencil that opens the intro dialog. Connect and Message
// request wait for connections (slice 13) and are not shown. Once the card scrolls out of
// view, a slim bar under the top bar repeats the name and buttons.
export function ProfileHeader({
  profile,
  edit,
  onContact,
}: ProfileHeaderProps) {
  const meta = metaLine(profile);
  const card = useRef<HTMLDivElement>(null);
  const pinned = useScrolledPast(card);
  const avatar = (
    <Avatar
      name={profile.fullName}
      src={profile.photoUrl}
      size="lg"
      className="border-4 border-rs-surface ring-1 ring-rs-nav-border"
    />
  );
  return (
    <>
      <div ref={card} className={`${homeCard} overflow-hidden font-rs-ui`}>
        <Cover
          src={profile.coverUrl}
          className="h-[200px] max-[640px]:h-[120px]"
        >
          {edit && (
            <button
              id={editButtonId("cover")}
              type="button"
              aria-label="Edit cover image"
              onClick={() => edit.open("cover")}
              className="absolute top-3 right-3 rounded-full bg-rs-surface p-2 text-rs-accent-strong shadow-popover transition-colors hover:bg-rs-accent-soft focus-visible:shadow-focus focus-visible:outline-none"
            >
              <CameraIcon />
            </button>
          )}
        </Cover>
        <div className="flex flex-col gap-1.5 px-6 pb-5">
          <div className="relative -mt-[76px] self-start max-[640px]:-mt-[52px]">
            {edit ? (
              <button
                id={editButtonId("photo")}
                type="button"
                aria-label="Change profile photo"
                onClick={() => edit.open("photo")}
                className="block rounded-full focus-visible:shadow-focus focus-visible:outline-none"
              >
                {avatar}
              </button>
            ) : (
              avatar
            )}
          </div>
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0">
              <h1 className="text-rs-h3 leading-rs-tight font-bold break-words text-rs-text">
                {profile.fullName}
              </h1>
              {profile.headline && (
                <p className="mt-1 text-rs-lead leading-[1.4] text-rs-text">
                  {profile.headline}
                </p>
              )}
              {(meta || onContact) && (
                <p className="mt-1 text-rs-body text-rs-text-muted">
                  {meta}
                  {meta && onContact && " · "}
                  {onContact && (
                    <button
                      id={editButtonId("contact")}
                      type="button"
                      onClick={onContact}
                      className="rounded-rs-2xs font-medium text-rs-accent hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none"
                    >
                      Contact info
                    </button>
                  )}
                </p>
              )}
            </div>
            {edit && (
              <EditButton
                id={editButtonId("intro")}
                label="Edit intro"
                onClick={() => edit.open("intro")}
              />
            )}
          </div>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <MoreMenu userId={profile.userId} />
          </div>
        </div>
      </div>
      {pinned && <StickyBar profile={profile} edit={edit} />}
    </>
  );
}

// metaLine is the muted line under the headline, as in the design: "Research Assistant,
// Dept. of CSE · BUET · Dhaka, Bangladesh". Parts that are not set are left out.
function metaLine(profile: Profile): string {
  return [
    [profile.jobTitle, profile.department].filter(Boolean).join(", "),
    profile.institution,
    locationLine(profile),
  ]
    .filter(Boolean)
    .join(" · ");
}

// useScrolledPast is true once the element has scrolled up behind the top bar. Without
// IntersectionObserver it stays false.
function useScrolledPast(target: RefObject<HTMLElement | null>): boolean {
  const [past, setPast] = useState(false);
  useEffect(() => {
    const el = target.current;
    if (!el || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      ([entry]) =>
        setPast(
          !entry.isIntersecting && entry.boundingClientRect.top < topBarHeight,
        ),
      { rootMargin: `-${topBarHeight}px 0px 0px 0px` },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [target]);
  return past;
}

// StickyBar is the slim bar under the top bar while the header card is out of view.
function StickyBar({
  profile,
  edit,
}: Pick<ProfileHeaderProps, "profile" | "edit">) {
  return (
    <div
      role="region"
      aria-label="Profile summary"
      className="fixed inset-x-0 top-[52px] z-40 border-b border-rs-nav-border bg-rs-surface shadow-rs-bar max-[480px]:top-14"
    >
      <Container size="app" className="flex h-14 items-center gap-3">
        <Avatar
          name={profile.fullName}
          src={profile.photoUrl}
          size="bar"
          decorative
        />
        <div className="min-w-0 flex-1">
          <p className="truncate text-rs-body font-bold text-rs-text">
            {profile.fullName}
          </p>
          {profile.headline && (
            <p className="truncate text-rs-caption text-rs-text-muted">
              {profile.headline}
            </p>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {edit && (
            <EditButton label="Edit intro" onClick={() => edit.open("intro")} />
          )}
          <MoreMenu userId={profile.userId} variant="icon" />
        </div>
      </Container>
    </div>
  );
}
