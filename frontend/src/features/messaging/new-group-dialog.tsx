"use client";

import { useRouter } from "next/navigation";
import { type FormEvent, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Field, FormError } from "@/components/ui/field";

import { createGroup } from "./api";
import { bodyLength, waitText } from "./format";
import { MemberPicker, type Picked } from "./member-picker";
import { useConversations } from "./use-conversations";

// The limits of a group (FR-16.6): a name of 1 to 100 characters, and up to 50 members, the
// creator included.
export const maxGroupName = 100;
export const maxGroupSize = 50;

// groupNameError checks a name as the server does: 1 to 100 characters after trimming, on one
// line.
export function groupNameError(name: string): string | undefined {
  const length = bodyLength(name);
  if (length === 0) return "Give the group a name.";
  if (length > maxGroupName) return `The name can have at most ${maxGroupName} characters.`;
  if (/[\n\r\u2028\u2029]/.test(name)) return "The name must be on one line.";
  return undefined;
}

// NewGroupDialog creates a group of the caller's connections (FR-16.6) and opens it. A retry
// after a lost answer sends the same client ID, so it never makes a second group.
export function NewGroupDialog({ onClose }: { onClose: () => void }) {
  const router = useRouter();
  const list = useConversations();
  const [name, setName] = useState("");
  const [members, setMembers] = useState<Picked[]>([]);
  const [clientId] = useState(() => crypto.randomUUID());
  const [nameError, setNameError] = useState<string>();
  const [error, setError] = useState<string | null>(null);
  const [ineligible, setIneligible] = useState<ReadonlySet<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const length = bodyLength(name);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const invalid = groupNameError(name);
    setNameError(invalid);
    if (invalid) return;
    if (members.length === 0) {
      setError("Add at least one person.");
      return;
    }
    setError(null);
    setBusy(true);
    const res = await createGroup(name.trim(), members.map((m) => m.userId), clientId);
    setBusy(false);
    if (res.ok) {
      list.put("inbox", res.data);
      onClose();
      router.push(`/messaging/${res.data.id}`);
      return;
    }
    if (res.code === "ineligible_members") {
      setIneligible(new Set(res.userIds ?? []));
      setError("Some people can't be added. Remove them to create the group.");
    } else if (res.code === "invalid_group_name" || (res.code === "bad_request" && res.field === "name")) {
      setNameError(res.message);
    } else if (res.code === "group_full") {
      setError(`A group can have at most ${maxGroupSize} members.`);
    } else if (res.code === "rate_limited") {
      setError(`You've created the most groups allowed for now. Try again ${waitText(res.retryAfter)}.`);
    } else {
      setError(res.message);
    }
  }

  // Removing a refused person clears their mark; the others stay marked until the next try.
  function change(next: Picked[]) {
    setMembers(next);
    if (ineligible.size > 0) setIneligible(new Set([...ineligible].filter((id) => next.some((m) => m.userId === id))));
  }

  const blocked = members.some((m) => ineligible.has(m.userId));

  return (
    <Dialog title="New group" dirty={name !== "" || members.length > 0} onClose={onClose} returnFocusId="new-group">
      <form noValidate onSubmit={submit} className="flex flex-col gap-3.5">
        {error && <FormError look="rs">{error}</FormError>}
        <Field
          id="group-name"
          label="Group name"
          look="rs"
          required
          value={name}
          error={nameError}
          hint={`${length} / ${maxGroupName}`}
          onChange={(e) => setName(e.target.value)}
        />
        <MemberPicker selected={members} onChange={change} max={maxGroupSize - 1} ineligible={ineligible} disabled={busy} />
        <div className="flex flex-wrap justify-end gap-2 pt-1">
          <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
            Cancel
          </button>
          <button type="submit" disabled={busy || blocked || length > maxGroupName} className={rsButtonClass({ size: "form" })}>
            {busy ? "Creating…" : "Create"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
