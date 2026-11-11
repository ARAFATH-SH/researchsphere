import Link from "next/link";

import type { Conversation } from "./api";
import { rowPreview, rowTime } from "./format";
import { isGroup } from "./kinds";
import { PresenceAvatar } from "./presence-avatar";
import { StackedAvatars } from "./stacked-avatars";
import { firstName } from "./system-events";
import { useTypers } from "./typing";
import { TypingIndicator } from "./typing-indicator";

type ConversationRowProps = { conversation: Conversation; me: string; current: boolean };

// ConversationRow is one row of the list: the other person, or a group with its first members'
// avatars (FR-16.6). An unread row is bold and has its count, so unread is never told by colour
// alone.
export function ConversationRow({ conversation, me, current }: ConversationRowProps) {
  const { unreadCount } = conversation;
  const unread = unreadCount > 0;
  const typers = useTypers(conversation.id);
  const group = isGroup(conversation) ? conversation.group : null;
  const other = conversation.otherMember;
  const name = group?.name ?? other?.fullName ?? "";
  // A row knows a group's first members only; anyone else typing is "Someone".
  const typerNames = typers.map((id) =>
    group ? firstName(group.avatars.find((a) => a.userId === id)?.fullName ?? "Someone") : (other?.fullName ?? ""),
  );
  return (
    <li>
      <Link
        href={`/messaging/${conversation.id}`}
        aria-current={current ? "page" : undefined}
        className={`flex items-center gap-2.5 border-b border-l-[3px] border-b-rs-divider px-3 py-2.5 hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none ${
          current ? "border-l-rs-accent-strong bg-rs-accent-soft" : "border-l-transparent"
        }`}
      >
        {group ? (
          <StackedAvatars avatars={group.avatars} name={group.name} size="row" />
        ) : (
          other && <PresenceAvatar userId={other.userId} name={other.fullName} src={other.photoUrl} size="post" />
        )}
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="flex justify-between gap-1.5">
            <strong className="truncate text-rs-body text-rs-text">{name}</strong>
            <span className="flex-none text-rs-caption text-rs-text-muted">{rowTime(conversation.lastMessageAt)}</span>
          </span>
          <span className="flex items-center justify-between gap-1.5">
            {typers.length > 0 ? (
              <TypingIndicator names={typerNames} />
            ) : (
              <span className={`truncate text-rs-caption text-rs-text ${unread ? "font-bold" : ""}`}>{rowPreview(conversation, me)}</span>
            )}
            {unread && (
              <span className="flex h-5 min-w-5 flex-none items-center justify-center rounded-rs-pill bg-rs-accent-strong px-[5px] text-rs-tiny font-bold text-rs-surface">
                <span aria-hidden="true">{unreadCount > 99 ? "99+" : unreadCount}</span>
                <span className="sr-only">{unreadCount} unread</span>
              </span>
            )}
          </span>
        </span>
      </Link>
    </li>
  );
}
