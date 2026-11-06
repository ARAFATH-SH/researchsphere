import Link from "next/link";
import type { ReactNode } from "react";

import { Avatar } from "@/components/avatar";

import type { ConversationMember } from "./api";

type ThreadHeaderProps = {
  person: Pick<ConversationMember, "userId" | "fullName" | "photoUrl" | "headline">;
  // actions sits at the end of the row; 14A.6 puts its block and report menu here.
  actions?: ReactNode;
};

// ThreadHeader names the other person and links to their profile. Under 640px, where the
// thread is a screen of its own, it starts with the way back to the list.
export function ThreadHeader({ person, actions }: ThreadHeaderProps) {
  return (
    <div className="flex items-center gap-2.5 border-b border-rs-divider px-4 py-2.5">
      <Link
        href="/messaging"
        aria-label="Back to conversations"
        className="-ml-2 flex size-11 flex-none items-center justify-center rounded-full text-rs-text hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none min-[641px]:hidden"
      >
        <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className="size-5">
          <path d="M15 5l-7 7 7 7" />
        </svg>
      </Link>
      <Avatar name={person.fullName} src={person.photoUrl} size="thread" decorative />
      <div className="min-w-0 flex-1">
        <Link
          href={`/profile/${person.userId}`}
          className="block truncate rounded-rs-2xs text-rs-body font-bold text-rs-text hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none"
        >
          {person.fullName}
        </Link>
        {person.headline && <p className="truncate text-rs-caption text-rs-text-muted">{person.headline}</p>}
      </div>
      {actions}
    </div>
  );
}
