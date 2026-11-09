"use client";

import { type FormEvent, type ReactNode, useEffect, useId, useRef, useState } from "react";

import { Avatar } from "@/components/avatar";
import { rsButtonClass } from "@/components/ui/button";
import { Field, FormError } from "@/components/ui/field";

import {
  addGroupMembers,
  type Conversation,
  type GroupConversation,
  type GroupMember,
  leaveGroup,
  removeGroupMember,
  renameGroup,
} from "./api";
import { bodyLength, waitText } from "./format";
import { MemberPicker, type Picked } from "./member-picker";
import { groupNameError, maxGroupName, maxGroupSize } from "./new-group-dialog";

type GroupInfoPanelProps = {
  conversation: GroupConversation;
  me: string;
  // onChanged stores the group an action returned; onLeft follows the caller leaving it.
  onChanged: (conversation: Conversation) => void;
  onLeft: () => void;
  onReport: () => void;
  onClose: () => void;
};

// Step is what the panel shows: the members, or one of the owner's or member's actions.
type Step = { kind: "members" } | { kind: "rename" } | { kind: "add" } | { kind: "remove"; member: GroupMember } | { kind: "leave" };

// nextOwner is who becomes the owner when the owner leaves: the member who joined earliest, then
// the lower user ID, as the server's trigger decides (FR-16.6).
export function nextOwner(members: GroupMember[], me: string): GroupMember | null {
  const others = members.filter((m) => m.userId !== me);
  others.sort((a, b) => (a.joinedAt === b.joinedAt ? (a.userId < b.userId ? -1 : 1) : Date.parse(a.joinedAt) - Date.parse(b.joinedAt)));
  return others[0] ?? null;
}

// GroupInfoPanel is a group's members and actions (FR-16.6): a drawer on the right of the
// thread, a screen of its own on a phone. Everyone sees the members with their roles, leaves and
// reports; the owner also renames, adds and removes. Each change asks the server, which decides.
export function GroupInfoPanel({ conversation, me, onChanged, onLeft, onReport, onClose }: GroupInfoPanelProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const [step, setStep] = useState<Step>({ kind: "members" });
  const group = conversation.group;
  const members = group.members ?? [];
  const owner = group.myRole === "owner";

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (!el.open) el.showModal();
    return () => {
      if (el.open) el.close();
    };
  }, []);

  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onClick={(e) => {
        if (e.target === ref.current) onClose();
      }}
      className="m-0 h-dvh max-h-dvh w-full max-w-full flex-col overflow-hidden border-rs-border bg-rs-surface p-0 text-rs-body text-rs-text backdrop:bg-rs-text/40 open:flex min-[641px]:ml-auto min-[641px]:w-[380px] min-[641px]:border-l min-[641px]:shadow-card"
    >
      <div className="flex shrink-0 items-center justify-between gap-4 border-b border-rs-divider py-3 pr-3 pl-5">
        <h2 id={titleId} className="min-w-0 truncate text-rs-lead leading-rs-snug font-medium">
          Group info
        </h2>
        <button
          type="button"
          aria-label="Close"
          onClick={onClose}
          className="rounded-rs-pill p-1.5 text-rs-text-muted hover:bg-rs-subtle hover:text-rs-text focus-visible:shadow-focus focus-visible:outline-none"
        >
          <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.75} strokeLinecap="round" className="size-6">
            <path d="M6 6l12 12M18 6 6 18" />
          </svg>
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-5">
        {step.kind === "members" && (
          <Members
            name={group.name}
            count={group.memberCount}
            members={members}
            me={me}
            owner={owner}
            onRename={() => setStep({ kind: "rename" })}
            onAdd={() => setStep({ kind: "add" })}
            onRemove={(member) => setStep({ kind: "remove", member })}
            onLeave={() => setStep({ kind: "leave" })}
            onReport={onReport}
          />
        )}
        {step.kind === "rename" && (
          <Rename
            conversation={conversation}
            onDone={(next) => {
              onChanged(next);
              setStep({ kind: "members" });
            }}
            onCancel={() => setStep({ kind: "members" })}
          />
        )}
        {step.kind === "add" && (
          <AddMembers
            conversation={conversation}
            onDone={(next) => {
              onChanged(next);
              setStep({ kind: "members" });
            }}
            onCancel={() => setStep({ kind: "members" })}
          />
        )}
        {step.kind === "remove" && (
          <Confirm
            title={`Remove ${step.member.fullName}?`}
            text={`${step.member.fullName} will no longer see this group or its messages.`}
            action="Remove"
            run={() => removeGroupMember(conversation.id, step.member.userId)}
            onDone={() => {
              onChanged({
                ...conversation,
                group: {
                  ...group,
                  memberCount: Math.max(1, group.memberCount - 1),
                  members: members.filter((m) => m.userId !== step.member.userId),
                },
              });
              setStep({ kind: "members" });
            }}
            onCancel={() => setStep({ kind: "members" })}
          />
        )}
        {step.kind === "leave" && (
          <Confirm
            title="Leave this group?"
            text={leaveText(owner, members, me)}
            action="Leave"
            run={() => leaveGroup(conversation.id)}
            onDone={onLeft}
            onCancel={() => setStep({ kind: "members" })}
          />
        )}
      </div>
    </dialog>
  );
}

function leaveText(owner: boolean, members: GroupMember[], me: string): string {
  const base = "You will no longer see its messages, and you can come back only if the owner adds you again.";
  if (!owner) return base;
  const next = nextOwner(members, me);
  return next ? `${base} ${next.fullName} will become the owner.` : `${base} You are its last member, so the group will close.`;
}

type MembersProps = {
  name: string;
  count: number;
  members: GroupMember[];
  me: string;
  owner: boolean;
  onRename: () => void;
  onAdd: () => void;
  onRemove: (member: GroupMember) => void;
  onLeave: () => void;
  onReport: () => void;
};

function Members({ name, count, members, me, owner, onRename, onAdd, onRemove, onLeave, onReport }: MembersProps) {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-rs-lead font-bold break-words text-rs-text">{name}</p>
          <p className="text-rs-caption text-rs-text-muted">
            {count} / {maxGroupSize} members
          </p>
        </div>
        {owner && (
          <button type="button" onClick={onRename} className={rsButtonClass({ variant: "secondary", size: "xs" })}>
            Rename
          </button>
        )}
      </div>
      <ul aria-label="Members" className="flex flex-col">
        {members.map((m) => (
          <li key={m.userId} className="flex items-center gap-2.5 border-b border-rs-divider py-2 last:border-b-0">
            <Avatar name={m.fullName} src={m.available ? m.photoUrl : null} size="bubble" decorative />
            <span className="flex min-w-0 flex-1 flex-col">
              <span className="truncate text-rs-body text-rs-text">
                {m.fullName}
                {m.userId === me && <span className="text-rs-text-muted"> (you)</span>}
              </span>
              <span className="text-rs-caption text-rs-text-muted">{m.role === "owner" ? "Owner" : "Member"}</span>
            </span>
            {m.role === "owner" && (
              <span className="flex-none rounded-rs-pill bg-rs-accent-soft px-2 py-0.5 text-rs-tiny font-bold text-rs-accent-deep">Owner</span>
            )}
            {owner && m.userId !== me && (
              <button
                type="button"
                aria-label={`Remove ${m.fullName}`}
                onClick={() => onRemove(m)}
                className={rsButtonClass({ variant: "secondary", size: "xs" })}
              >
                Remove
              </button>
            )}
          </li>
        ))}
      </ul>
      {owner && count < maxGroupSize && (
        <button
          type="button"
          onClick={onAdd}
          className="min-h-11 rounded-rs-md border border-dashed border-rs-accent-strong text-rs-body font-medium text-rs-accent-strong hover:bg-rs-accent-soft focus-visible:shadow-focus focus-visible:outline-none"
        >
          + Add members
        </button>
      )}
      <div className="flex flex-wrap gap-2 border-t border-rs-divider pt-4">
        <button type="button" onClick={onLeave} className={rsButtonClass({ variant: "danger", size: "xs" })}>
          Leave group
        </button>
        <button type="button" onClick={onReport} className={rsButtonClass({ variant: "danger", size: "xs" })}>
          Report group
        </button>
      </div>
    </div>
  );
}

function Rename({ conversation, onDone, onCancel }: { conversation: GroupConversation; onDone: (c: Conversation) => void; onCancel: () => void }) {
  const [name, setName] = useState(conversation.group.name);
  const [error, setError] = useState<string>();
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const length = bodyLength(name);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const invalid = groupNameError(name);
    setError(invalid);
    if (invalid) return;
    setBusy(true);
    setFailure(null);
    const res = await renameGroup(conversation.id, name.trim());
    setBusy(false);
    if (res.ok) onDone(res.data);
    else if (res.code === "invalid_group_name" || res.field === "name") setError(res.message);
    else setFailure(res.message);
  }

  return (
    <form noValidate onSubmit={submit} className="flex flex-col gap-3.5">
      <h3 className="text-rs-body font-bold">Rename the group</h3>
      {failure && <FormError look="rs">{failure}</FormError>}
      <Field
        id="group-rename"
        label="Group name"
        look="rs"
        required
        value={name}
        error={error}
        hint={`${length} / ${maxGroupName}`}
        onChange={(e) => setName(e.target.value)}
      />
      <Actions busy={busy} label="Save" onCancel={onCancel} />
    </form>
  );
}

function AddMembers({ conversation, onDone, onCancel }: { conversation: GroupConversation; onDone: (c: Conversation) => void; onCancel: () => void }) {
  const [picked, setPicked] = useState<Picked[]>([]);
  const [ineligible, setIneligible] = useState<ReadonlySet<string>>(new Set());
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const current = new Set((conversation.group.members ?? []).map((m) => m.userId));
  const room = maxGroupSize - conversation.group.memberCount;

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (picked.length === 0) {
      setFailure("Choose at least one person.");
      return;
    }
    setBusy(true);
    setFailure(null);
    const res = await addGroupMembers(conversation.id, picked.map((p) => p.userId));
    setBusy(false);
    if (res.ok) {
      onDone(res.data);
      return;
    }
    if (res.code === "ineligible_members") {
      setIneligible(new Set(res.userIds ?? []));
      setFailure("Some people can't be added. Remove them to continue.");
    } else if (res.code === "group_full") {
      setFailure(`A group can have at most ${maxGroupSize} members.`);
    } else if (res.code === "rate_limited") {
      setFailure(`You've added the most members allowed for now. Try again ${waitText(res.retryAfter)}.`);
    } else {
      setFailure(res.message);
    }
  }

  function change(next: Picked[]) {
    setPicked(next);
    if (ineligible.size > 0) setIneligible(new Set([...ineligible].filter((id) => next.some((p) => p.userId === id))));
  }

  return (
    <form noValidate onSubmit={submit} className="flex flex-col gap-3.5">
      <h3 className="text-rs-body font-bold">Add members</h3>
      {failure && <FormError look="rs">{failure}</FormError>}
      <MemberPicker selected={picked} onChange={change} max={room} exclude={current} ineligible={ineligible} disabled={busy} />
      <Actions busy={busy || picked.some((p) => ineligible.has(p.userId))} label="Add" onCancel={onCancel} />
    </form>
  );
}

type ConfirmProps = {
  title: string;
  text: string;
  action: string;
  run: () => Promise<{ ok: boolean; message?: string }>;
  onDone: () => void;
  onCancel: () => void;
};

// Confirm asks before a removal or leaving, inside the panel (docs/ui-guidelines.md).
function Confirm({ title, text, action, run, onDone, onCancel }: ConfirmProps) {
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);
  useEffect(() => confirmRef.current?.focus(), []);

  async function confirm() {
    setBusy(true);
    setFailure(null);
    const res = await run();
    setBusy(false);
    if (res.ok) onDone();
    else setFailure(res.message ?? "Something went wrong. Try again.");
  }

  return (
    <div role="alertdialog" aria-labelledby="group-confirm-title" aria-describedby="group-confirm-text" className="flex flex-col gap-3">
      <p id="group-confirm-title" className="text-rs-lead font-medium">
        {title}
      </p>
      <p id="group-confirm-text" className="text-rs-text-muted">
        {text}
      </p>
      {failure && <FormError look="rs">{failure}</FormError>}
      <div className="flex flex-wrap justify-end gap-2 pt-1">
        <button type="button" onClick={onCancel} className={rsButtonClass({ variant: "secondary", size: "form" })}>
          Cancel
        </button>
        <button ref={confirmRef} type="button" disabled={busy} onClick={confirm} className={rsButtonClass({ variant: "destructive", size: "form" })}>
          {action}
        </button>
      </div>
    </div>
  );
}

function Actions({ busy, label, onCancel }: { busy: boolean; label: ReactNode; onCancel: () => void }) {
  return (
    <div className="flex flex-wrap justify-end gap-2 pt-1">
      <button type="button" onClick={onCancel} className={rsButtonClass({ variant: "secondary", size: "form" })}>
        Cancel
      </button>
      <button type="submit" disabled={busy} className={rsButtonClass({ size: "form" })}>
        {label}
      </button>
    </div>
  );
}
