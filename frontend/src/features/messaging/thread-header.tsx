import Link from "next/link";
import type { ReactNode } from "react";

import type { ConversationMember, GroupAvatar } from "./api";
import { PresenceAvatar } from "./presence-avatar";
import { StackedAvatars } from "./stacked-avatars";
import { TypingIndicator } from "./typing-indicator";

// HeaderSubject is who the thread is with: one person, or a group (FR-16.6).
export type HeaderSubject =
  | { kind: "person"; person: Pick<ConversationMember, "userId" | "fullName" | "photoUrl" | "headline"> }
  | { kind: "group"; name: string; memberCount: number; avatars: GroupAvatar[] };

type ThreadHeaderProps = {
  subject: HeaderSubject;
  // typing names who are typing, in place of the headline or the member count.
  typing?: string[];
  // actions sits at the end of the row: the thread's pills.
  actions?: ReactNode;
  // onOpenInfo opens a group's info panel from its name.
  onOpenInfo?: () => void;
};

// ThreadHeader names the other person and links to their profile, or names a group with its
// size and first members, opening its info. Under 640px, where the thread is a screen of its
// own, it starts with the way back to the list.
export function ThreadHeader({ subject, typing = [], actions, onOpenInfo }: ThreadHeaderProps) {
  const typingNow = typing.length > 0;
  return (
    <div className="flex flex-wrap items-center gap-2.5 border-b border-rs-divider px-4 py-2.5">
      <Link
        href="/messaging"
        aria-label="Back to conversations"
        className="-ml-2 flex size-11 flex-none items-center justify-center rounded-full text-rs-text hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none min-[641px]:hidden"
      >
        <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className="size-5">
          <path d="M15 5l-7 7 7 7" />
        </svg>
      </Link>
      {subject.kind === "person" ? (
        <PresenceAvatar userId={subject.person.userId} name={subject.person.fullName} src={subject.person.photoUrl} size="thread" />
      ) : (
        <StackedAvatars avatars={subject.avatars} name={subject.name} size="thread" />
      )}
      <div className="min-w-[160px] flex-1">
        {subject.kind === "person" ? (
          <Link
            href={`/profile/${subject.person.userId}`}
            className="block truncate rounded-rs-2xs text-rs-body font-bold text-rs-text hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none"
          >
            {subject.person.fullName}
          </Link>
        ) : (
          <button
            type="button"
            onClick={onOpenInfo}
            className="block max-w-full truncate rounded-rs-2xs text-left text-rs-body font-bold text-rs-text hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none"
          >
            {subject.name}
          </button>
        )}
        {!typingNow && subject.kind === "person" && subject.person.headline && (
          <p className="truncate text-rs-caption text-rs-text-muted">{subject.person.headline}</p>
        )}
        {!typingNow && subject.kind === "group" && (
          <p className="truncate text-rs-caption text-rs-text-muted">
            {subject.memberCount} {subject.memberCount === 1 ? "member" : "members"}
          </p>
        )}
        <div role="status">{typingNow && <TypingIndicator names={typing} />}</div>
      </div>
      {actions}
    </div>
  );
}
