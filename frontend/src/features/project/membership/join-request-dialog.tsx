"use client";

import { useId, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { FormError, TextAreaField } from "@/components/ui/field";

import { createJoinRequest, maxJoinMessageLength, membershipErrorText, type MyJoinRequest } from "../api";

// JoinRequestDialog asks to join an open project with an optional message to the owner of at
// most 500 characters (FR-11.1). onConflict hears a refusal that means the page is out of date
// (a request or invitation exists already, or the project closed), so it can read again.
export function JoinRequestDialog({
  projectId,
  ownerName,
  onSent,
  onConflict,
  onClose,
  returnFocusId,
}: {
  projectId: string;
  ownerName: string;
  onSent: (request: MyJoinRequest) => void;
  onConflict: (code: string) => void;
  onClose: () => void;
  returnFocusId?: string;
}) {
  const id = useId();
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const length = [...message.trim()].length;
  const tooLong = length > maxJoinMessageLength;

  async function send() {
    if (tooLong || busy) return;
    setBusy(true);
    setError(null);
    const res = await createJoinRequest(projectId, message);
    if (res.ok) {
      onSent(res.data);
      return;
    }
    setBusy(false);
    setError(membershipErrorText(res.code, "We couldn’t send your request. Try again."));
    if (["request_pending", "invitation_pending", "project_not_open", "already_member"].includes(res.code)) onConflict(res.code);
  }

  return (
    <Dialog title="Request to join" dirty={message.trim() !== "" && !busy} onClose={onClose} returnFocusId={returnFocusId}>
      <form
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
        className="flex flex-col gap-4"
      >
        <TextAreaField
          id={id}
          look="rs"
          label={`Message to ${ownerName} (optional)`}
          rows={4}
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          error={tooLong ? `Keep the message to ${maxJoinMessageLength} characters.` : undefined}
          counter={{ count: length, max: maxJoinMessageLength }}
        />
        <p className="text-rs-caption text-rs-text-muted">
          You can have one pending request for this project. The workspace and files stay hidden until you’re a member.
        </p>
        {error && <FormError look="rs">{error}</FormError>}
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
            Cancel
          </button>
          <button type="submit" disabled={busy || tooLong} className={rsButtonClass({ size: "form" })}>
            Send request
          </button>
        </div>
      </form>
    </Dialog>
  );
}
