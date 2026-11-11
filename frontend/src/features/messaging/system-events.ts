import type { MessagePerson, SystemEvent, SystemEventType } from "./api";

// The sentences of a group's system messages (FR-16.6). The API sends an event key and the
// people it names, never English; these tables are the client's one place for the words.

// deletedAccount names someone whose account is gone: they are missing from the page's people.
export const deletedAccount = "Deleted account";

// PersonLookup finds a person a page names, or undefined when their account is gone.
export type PersonLookup = (userId: string) => Pick<MessagePerson, "fullName"> | undefined;

// firstName is the part of a full name before the first space, for a list row's preview.
export function firstName(fullName: string): string {
  return fullName.trim().split(/\s+/)[0] || fullName;
}

// nameList joins names as a sentence does: "A", "A and B", "A, B and C".
export function nameList(names: string[]): string {
  if (names.length <= 1) return names[0] ?? "";
  return `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
}

// eventSentence is a system message's line in the thread: "Arafath added Nafisa and Rahim",
// "You renamed the group from Lab to Lab 2", "Nafisa is now the owner". The actor is the
// message's sender (null for an ownership hand-over); the caller is "You".
export function eventSentence(event: SystemEvent, actorId: string | null, me: string, find: PersonLookup): string {
  const name = (id: string, start: boolean) => (id === me ? (start ? "You" : "you") : (find(id)?.fullName ?? deletedAccount));
  const actor = actorId ? name(actorId, true) : "Someone";
  const users = nameList(event.userIds.map((id) => name(id, false)));
  switch (event.type) {
    case "group_created":
      return `${actor} created the group`;
    case "group_renamed":
      return `${actor} renamed the group from ${event.oldName ?? "its old name"} to ${event.newName ?? "a new name"}`;
    case "member_added":
      return `${actor} added ${users}`;
    case "member_removed":
      return `${actor} removed ${users}`;
    case "member_left": {
      const who = event.userIds[0] ?? actorId;
      return `${who ? name(who, true) : "Someone"} left`;
    }
    case "owner_changed": {
      const owner = event.userIds[0];
      if (!owner) return "The group has a new owner";
      return owner === me ? "You are now the owner" : `${name(owner, true)} is now the owner`;
    }
  }
}

const previews: Record<SystemEventType, (actor: string) => string> = {
  group_created: (actor) => `${actor} created the group`,
  group_renamed: (actor) => `${actor} renamed the group`,
  member_added: (actor) => `${actor} added members`,
  member_removed: (actor) => `${actor} removed a member`,
  member_left: (actor) => `${actor} left`,
  owner_changed: () => "The group has a new owner",
};

// eventPreview is a system message as a list row says it, from the key and the actor alone (a
// row has no people): "Nafisa renamed the group", "You added members".
export function eventPreview(event: SystemEventType, senderId: string | null, senderName: string | null, me: string): string {
  const actor = senderId === me ? "You" : senderName ? firstName(senderName) : "Someone";
  return previews[event](actor);
}
