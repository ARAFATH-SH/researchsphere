"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { type RefObject, useEffect, useRef, useState } from "react";

import { Avatar } from "@/components/avatar";
import { Container } from "@/components/Container";
import { useSession } from "@/features/auth/session";
import { homeCard } from "@/features/home/cards";
import { MessageButton, messageAction } from "@/features/messaging/message-button";
import { ConnectButton, type Link as ConnectionLink } from "@/features/social/connect-button";

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
// the cover, a clickable photo and a pencil that opens the intro dialog. Under the meta line
// "N connections" links to the connections list (FR-7.4). On someone else's profile the
// action row has the message button (FR-16.1, FR-16.2), the connect button (FR-7.1 to FR-7.3)
// and More, which offers "Remove connection" once connected (FR-7.5). A connection with no
// message button, because their conversation is closed, keeps a "Connected" label. Once the card scrolls
// out of view, a slim bar under the top bar repeats the name and buttons.
export function ProfileHeader({
  profile,
  edit,
  onContact,
}: ProfileHeaderProps) {
  const meta = metaLine(profile);
  const connection = useConnection(profile);
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
              {connection.count > 0 && (
                <p className="mt-1 text-rs-body">
                  <Link
                    href={connection.self ? "/network" : `/profile/${profile.userId}/connections`}
                    className="rounded-rs-2xs font-medium text-rs-accent hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none"
                  >
                    {connectionsLabel(connection.count)}
                  </Link>
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
          <div className="mt-2 flex flex-wrap items-start gap-2">
            <Actions profile={profile} connection={connection} variant="pill" />
          </div>
        </div>
      </div>
      {pinned && <StickyBar profile={profile} edit={edit} connection={connection} />}
    </>
  );
}

// connectionsLabel is "1 connection" or "N connections".
export function connectionsLabel(count: number): string {
  return count === 1 ? "1 connection" : `${count} connections`;
}

type Connection = ConnectionLink & {
  self: boolean;
  count: number;
  blockedByMe: boolean;
  change: (next: ConnectionLink) => void;
  setBlockedByMe: (next: boolean) => void;
};

// useConnection keeps the viewer's link with the profile's owner and the owner's
// connection count, so the card and the sticky bar agree and the count follows an accept
// or a remove without a reload. A new profile from the server starts it again.
function useConnection(profile: Profile): Connection {
  const initial = () => ({
    status: profile.connectionStatus,
    requestId: profile.connectionRequestId,
    count: profile.connectionCount,
    blockedByMe: profile.blockedByMe ?? false,
  });
  const [state, setState] = useState(initial);
  const [source, setSource] = useState(profile);
  if (source !== profile) {
    setSource(profile);
    setState(initial());
  }
  return {
    ...state,
    self: state.status === "self",
    change: (next) =>
      setState((prev) => {
        let count = prev.count;
        if (prev.status !== "connected" && next.status === "connected") count++;
        if (prev.status === "connected" && next.status !== "connected") count--;
        return { ...prev, ...next, count: Math.max(0, count) };
      }),
    setBlockedByMe: (blockedByMe) => setState((prev) => ({ ...prev, blockedByMe })),
  };
}

// Actions is the action row. Message comes first when it is the main action; next to a filled
// Connect, "Message request" follows it, as the design draws them. The slim bar has room for
// the filled Message only.
function Actions({
  profile,
  connection,
  variant,
}: {
  profile: Profile;
  connection: Connection;
  variant: "pill" | "icon";
}) {
  const session = useSession();
  const router = useRouter();
  const me = session.status === "signedIn" ? session.user.id : null;
  const connected = connection.status === "connected";
  const action = messageAction({ ...profile, blockedByMe: connection.blockedByMe }, connected, me);
  const message = action && (variant === "pill" || action.kind === "filled") ? <MessageButton action={action} /> : null;
  return (
    <>
      {action?.kind === "filled" && message}
      {!connection.self && (
        <ConnectButton
          userId={profile.userId}
          name={profile.fullName}
          status={connection.status}
          requestId={connection.requestId}
          size="profile"
          onChange={connection.change}
        />
      )}
      {action?.kind === "outline" && message}
      {connected && !action && variant === "pill" && <ConnectedLabel />}
      <MoreMenu
        userId={profile.userId}
        variant={variant}
        connected={
          connected
            ? {
                name: profile.fullName,
                onRemoved: () => connection.change({ status: "none", requestId: null }),
              }
            : undefined
        }
        blocking={
          connection.self
            ? undefined
            : {
                name: profile.fullName,
                blockedByMe: connection.blockedByMe,
                // The profile has nothing left to do for the viewer, so go back where they came from.
                onBlocked: () => router.back(),
                onUnblocked: () => connection.setBlockedByMe(false),
              }
        }
      />
    </>
  );
}

// ConnectedLabel says the viewer is connected. It is status text, not a control, so it
// has no border and takes no focus.
function ConnectedLabel() {
  return (
    <span className="inline-flex h-10 items-center gap-1.5 px-1 text-rs-body font-medium text-rs-text-muted">
      <svg
        aria-hidden="true"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth={2.2}
        strokeLinecap="round"
        strokeLinejoin="round"
        className="size-4 text-rs-accent-strong"
      >
        <path d="M5 12.5l4.5 4.5L19 7.5" />
      </svg>
      Connected
    </span>
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
  connection,
}: Pick<ProfileHeaderProps, "profile" | "edit"> & { connection: Connection }) {
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
          <Actions profile={profile} connection={connection} variant="icon" />
        </div>
      </Container>
    </div>
  );
}
