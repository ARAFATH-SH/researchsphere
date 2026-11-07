"use client";

import { useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/field";

import { blockUser, unblockUser } from "./api";

type BlockDialogProps = {
  action: "block" | "unblock";
  userId: string;
  name: string;
  onClose: () => void;
  onDone: () => void;
  returnFocusId?: string;
};

const effects = {
  block: "They won't be able to message you, see your profile or find you in search. Your connection will be removed. They won't be told.",
  unblock: "They'll be able to find you, see your profile and message you again. Your connection won't come back.",
};

// BlockDialog asks before a block or an unblock and says in plain words what it does (FR-16.9).
export function BlockDialog({ action, userId, name, onClose, onDone, returnFocusId }: BlockDialogProps) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const verb = action === "block" ? "Block" : "Unblock";

  async function confirm() {
    setBusy(true);
    setError(null);
    const res = await (action === "block" ? blockUser : unblockUser)(userId);
    setBusy(false);
    if (res.ok) onDone();
    else setError(res.message);
  }

  return (
    <Dialog title={`${verb} ${name}?`} onClose={onClose} returnFocusId={returnFocusId}>
      <div className="flex flex-col gap-3">
        <p className="text-rs-body text-rs-text">{effects[action]}</p>
        {error && <FormError look="rs">{error}</FormError>}
        <div className="flex flex-wrap justify-end gap-2 pt-1">
          <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
            Cancel
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={confirm}
            className={rsButtonClass({ variant: action === "block" ? "destructive" : "primary", size: "form" })}
          >
            {verb}
          </button>
        </div>
      </div>
    </Dialog>
  );
}
