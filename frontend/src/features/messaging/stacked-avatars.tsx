import { Avatar } from "@/components/avatar";

import type { GroupAvatar } from "./api";

type StackedAvatarsProps = {
  // avatars are up to 3 members other than the caller, those who joined earliest first.
  avatars: GroupAvatar[];
  // name is the group's, shown as initials when the group has nobody else yet.
  name: string;
  // size is the box the stack fills: a list row's (44px) or a thread header's (40px).
  size: "row" | "thread";
};

const box = { row: "size-11", thread: "size-10" };

// StackedAvatars is a group's picture (FR-16.6): its first members' avatars overlapping in one
// round box, so a group row lines up with a direct one. It is decorative; the name is beside it.
export function StackedAvatars({ avatars, name, size }: StackedAvatarsProps) {
  const shown = avatars.slice(0, 3);
  if (shown.length === 0) {
    return <Avatar name={name} src={null} size={size === "row" ? "post" : "thread"} decorative />;
  }
  if (shown.length === 1) {
    return <Avatar name={shown[0].fullName} src={shown[0].photoUrl} size={size === "row" ? "post" : "thread"} decorative />;
  }
  // Two or three: smaller circles, the first at the top left, the others below and to the right.
  const spots = shown.length === 2 ? ["top-0 left-0", "right-0 bottom-0"] : ["top-0 left-0", "top-0 right-0", "bottom-0 left-1/2 -translate-x-1/2"];
  return (
    <span aria-hidden="true" className={`relative flex-none ${box[size]}`}>
      {shown.map((a, i) => (
        <Avatar
          key={a.userId}
          name={a.fullName}
          src={a.photoUrl}
          size={shown.length === 2 ? "bubble" : "sm"}
          decorative
          className={`absolute ring-2 ring-rs-surface ${spots[i]}`}
        />
      ))}
    </span>
  );
}
