import { Avatar } from "@/components/avatar";

import type { PresenceUser } from "./collab/types";

export type MemberFace = { name: string; photoUrl: string | null };

// PresenceBar shows who edits the open file (FR-12.5), as design/screens/Workspace.html draws
// it in the workspace bar: overlapping avatars, each ringed in the member's cursor colour, and
// "3 / 4 editing". Each avatar's tooltip names the member. Under 640px only the count shows.
export function PresenceBar({
  users,
  maxEditors,
  selfId,
  faces,
}: {
  users: PresenceUser[];
  maxEditors: number | null;
  selfId: string | null;
  faces: ReadonlyMap<string, MemberFace>;
}) {
  if (users.length === 0) return null;
  const editing = users.filter((u) => u.readOnly === null).length;
  const count = maxEditors ? `${editing} / ${maxEditors} editing` : `${editing} editing`;
  return (
    <div role="group" aria-label={`${editing} ${editing === 1 ? "person" : "people"} editing this file`} className="flex items-center">
      <ul className="flex items-center max-[640px]:hidden">
        {users.map((u, i) => {
          const face = faces.get(u.id);
          const name = face?.name || u.name || "A member";
          const title = `${name}${u.id === selfId ? " (you)" : ""}${u.readOnly === "capacity" ? ", waiting to edit" : ""}`;
          return (
            <li
              key={u.id}
              title={title}
              style={{ boxShadow: `0 0 0 2px var(--rs-surface), 0 0 0 4px ${u.colour}` }}
              className={`relative rounded-full ${i > 0 ? "-ml-1" : ""} ${u.readOnly === "capacity" ? "opacity-60" : ""}`}
            >
              <Avatar name={title} src={face?.photoUrl ?? null} size="bubble" />
            </li>
          );
        })}
      </ul>
      <span className="ml-2.5 text-rs-caption max-[640px]:ml-0">{count}</span>
    </div>
  );
}
