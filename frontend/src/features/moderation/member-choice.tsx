"use client";

import { type FormEvent, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/field";

import type { MessageReportPerson } from "./api";

type MemberChoiceProps = {
  // members are who was in the group at the moment of the report, also those who left since.
  members: MessageReportPerson[];
  me: string | null;
  // action names what follows: "Warn", "Restrict messaging", "Suspend".
  action: string;
  onChoose: (member: MessageReportPerson) => void;
  onClose: () => void;
};

// MemberChoice is the first step of an action on a report of a whole group (FR-16.6), which names
// nobody: "Who is this action for?", among the members at the report's moment. Administrators,
// oneself included, cannot be moderated (FR-15.10), so they are listed but cannot be chosen.
export function MemberChoice({ members, me, action, onChoose, onClose }: MemberChoiceProps) {
  const [chosen, setChosen] = useState("");
  const [error, setError] = useState<string | null>(null);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const member = members.find((m) => m.userId === chosen);
    if (!member) {
      setError("Choose a member.");
      return;
    }
    onChoose(member);
  }

  return (
    <Dialog title={action} onClose={onClose}>
      <form noValidate onSubmit={submit} className="flex flex-col gap-3.5">
        {error && <FormError look="rs">{error}</FormError>}
        <fieldset className="flex flex-col gap-1">
          <legend className="mb-1 text-rs-body font-medium text-rs-text">Who is this action for?</legend>
          {members.length === 0 && <p className="text-rs-body text-rs-text-muted">Nobody was in the group at the report&apos;s time.</p>}
          {members.map((m) => {
            const blocked = m.role === "admin" || m.userId === me;
            return (
              <label
                key={m.userId}
                className={`flex min-h-11 items-center gap-2.5 rounded-rs-md px-2 text-rs-body ${blocked ? "text-rs-text-muted" : "text-rs-text hover:bg-rs-subtle"}`}
              >
                <input
                  type="radio"
                  name="action-member"
                  value={m.userId}
                  checked={chosen === m.userId}
                  disabled={blocked}
                  onChange={() => setChosen(m.userId)}
                  className="size-[18px] accent-rs-accent-strong"
                />
                <span className="min-w-0 flex-1 truncate">{m.fullName}</span>
                {blocked && <span className="text-rs-caption">{m.userId === me ? "You" : "Administrator"}</span>}
              </label>
            );
          })}
        </fieldset>
        <div className="flex flex-wrap justify-end gap-2 pt-1">
          <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
            Cancel
          </button>
          <button type="submit" disabled={chosen === ""} className={rsButtonClass({ size: "form" })}>
            Continue
          </button>
        </div>
      </form>
    </Dialog>
  );
}
