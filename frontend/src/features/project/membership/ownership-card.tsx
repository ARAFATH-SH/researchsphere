"use client";

import { useId, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { rsSelectClass } from "@/components/ui/field";

import { type Project, transferProject } from "../api";
import { DeleteProjectDialog } from "../delete-project-dialog";
import { ConfirmDialog } from "./confirm-dialog";
import type { Membership } from "./use-membership";

const transferButtonId = "transfer-project";
const deleteButtonId = "delete-project";

// OwnershipCard is the owner's "Ownership & deletion" card (design/screens/ProjectDetail.html):
// transfer ownership to another member, after a dialog that says the owner becomes an editor
// (FR-10.5), and delete the project (FR-10.6).
export function OwnershipCard({
  project,
  membership,
  onTransferred,
  onDeleted,
  showToast,
}: {
  project: Project;
  membership: Membership;
  onTransferred: (project: Project) => void;
  onDeleted: () => void;
  showToast: (message: string) => void;
}) {
  const selectId = useId();
  const { members } = membership;
  const others = members.value.status === "ok" ? members.value.data.items.filter((m) => m.role !== "owner") : [];
  const [chosen, setChosen] = useState<string>("");
  const [open, setOpen] = useState<"transfer" | "delete" | null>(null);
  const target = others.find((m) => m.user.userId === chosen) ?? others[0] ?? null;

  return (
    <section
      aria-labelledby="ownership-h"
      className="flex flex-col gap-3 rounded-card border border-rs-danger-line bg-rs-surface p-4 shadow-card"
    >
      <h2 id="ownership-h" className="text-rs-lead leading-rs-snug font-medium text-rs-danger-strong">
        Ownership &amp; deletion
      </h2>
      <div className="flex flex-wrap items-end gap-2.5">
        <div className="flex min-w-[220px] flex-1 flex-col gap-1">
          <label htmlFor={selectId} className="text-rs-body font-medium text-rs-text">
            Transfer ownership to
          </label>
          <select
            id={selectId}
            value={target?.user.userId ?? ""}
            disabled={others.length === 0}
            onChange={(e) => setChosen(e.target.value)}
            className={rsSelectClass}
          >
            {others.length === 0 && <option value="">No other members yet</option>}
            {others.map((m) => (
              <option key={m.user.userId} value={m.user.userId}>
                {m.user.fullName} ({m.role})
              </option>
            ))}
          </select>
        </div>
        <button
          id={transferButtonId}
          type="button"
          disabled={!target}
          onClick={() => setOpen("transfer")}
          className={rsButtonClass({ variant: "outline", size: "md" })}
        >
          Transfer
        </button>
      </div>
      <div className="flex flex-wrap items-center gap-2.5 border-t border-rs-danger-line pt-3">
        <p className="min-w-[220px] flex-1 text-rs-body text-rs-text">
          Delete this project and all its files. Type the project title to confirm.
        </p>
        <button
          id={deleteButtonId}
          type="button"
          onClick={() => setOpen("delete")}
          className={rsButtonClass({ variant: "destructive", size: "md" })}
        >
          Delete project
        </button>
      </div>

      {open === "transfer" && target && (
        <ConfirmDialog
          title={`Make ${target.user.fullName} the owner?`}
          action="Transfer ownership"
          run={() => transferProject(project.id, target.user.userId)}
          onDone={(next) => {
            setOpen(null);
            showToast(`${target.user.fullName} is now the owner`);
            members.reload();
            onTransferred(next);
          }}
          onClose={() => setOpen(null)}
          returnFocusId={transferButtonId}
        >
          <p>
            {target.user.fullName} will own “{project.title}”: they manage its members and settings and can delete it.
            You become an editor. Only the new owner can give ownership back.
          </p>
        </ConfirmDialog>
      )}
      {open === "delete" && (
        <DeleteProjectDialog
          projectId={project.id}
          title={project.title}
          onClose={() => setOpen(null)}
          onDeleted={onDeleted}
          returnFocusId={deleteButtonId}
        />
      )}
    </section>
  );
}
