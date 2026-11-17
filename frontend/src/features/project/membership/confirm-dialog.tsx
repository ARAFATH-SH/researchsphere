"use client";

import { type ReactNode, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/field";
import type { Result } from "@/lib/api/call";

import { membershipErrorText } from "../api";

// ConfirmDialog asks before a membership change that cannot be undone from the page (remove a
// member, leave, transfer ownership). It sends the change itself and stays open with the
// reason when the server refuses.
export function ConfirmDialog<T>({
  title,
  children,
  action,
  destructive = false,
  run,
  onDone,
  onClose,
  returnFocusId,
}: {
  title: string;
  children: ReactNode;
  action: string;
  destructive?: boolean;
  run: () => Promise<Result<T>>;
  onDone: (data: T) => void;
  onClose: () => void;
  returnFocusId?: string;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function confirm() {
    setBusy(true);
    setError(null);
    const res = await run();
    if (res.ok) {
      onDone(res.data);
      return;
    }
    setBusy(false);
    setError(membershipErrorText(res.code));
  }

  return (
    <Dialog title={title} onClose={onClose} returnFocusId={returnFocusId}>
      <div className="flex flex-col gap-4">
        <div className="text-rs-body text-rs-text">{children}</div>
        {error && <FormError look="rs">{error}</FormError>}
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" onClick={onClose} className={rsButtonClass({ variant: "secondary", size: "form" })}>
            Cancel
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => void confirm()}
            className={rsButtonClass({ variant: destructive ? "destructive" : "primary", size: "form" })}
          >
            {action}
          </button>
        </div>
      </div>
    </Dialog>
  );
}
