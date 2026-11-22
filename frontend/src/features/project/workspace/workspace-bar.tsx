import Link from "next/link";
import type { ReactNode } from "react";

import type { ProjectRole } from "../api";

const roleLabels: Record<ProjectRole, string> = { owner: "Owner", editor: "Editor", viewer: "Viewer" };

const focusRing = "rounded-rs-xs focus-visible:shadow-[0_0_0_2px_var(--rs-surface)] focus-visible:outline-none";

export type BarStatus = {
  text: string;
  // detail follows the text, as the REST save's own status while live editing is unavailable.
  detail?: string;
  trouble: boolean;
  saved: boolean;
};

// WorkspaceBar is the workspace's own top bar (design/screens/Workspace.html): the logo to
// Home, the way back to the project, the caller's role, the open file's status (Connecting,
// Live, Saving, Saved, Offline) and who edits it (presence). Compile and the PDF come with 23.
export function WorkspaceBar({
  projectId,
  title,
  role,
  save,
  presence,
}: {
  projectId: string;
  title: string;
  role: ProjectRole;
  // save is the open text file's status; null when no text file is open or it is read-only.
  save: BarStatus | null;
  presence?: ReactNode;
}) {
  const saved = save?.saved;
  const trouble = save?.trouble;
  return (
    <header className="bg-rs-accent-strong text-rs-surface">
      <div className="flex min-h-14 flex-wrap items-center gap-3 px-4 py-1.5">
        <Link href="/home" aria-label="ResearchSphere home" className={`flex-none ${focusRing}`}>
          <span
            aria-hidden="true"
            className="flex size-[34px] items-center justify-center rounded-control bg-rs-surface text-rs-body font-bold text-rs-accent-strong"
          >
            RS
          </span>
        </Link>
        <Link
          href={`/projects/${projectId}`}
          className={`flex min-w-0 items-center gap-1.5 text-rs-lead font-medium text-rs-surface no-underline hover:underline ${focusRing}`}
        >
          <svg aria-hidden="true" viewBox="0 0 24 24" className="size-4 flex-none">
            <path d="M15 6l-6 6 6 6" fill="none" stroke="currentColor" strokeWidth={2.2} strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          <span className="sr-only">Back to project: </span>
          <span className="truncate">{title}</span>
        </Link>
        <span className="rounded-rs-pill bg-rs-accent-deep px-2.5 py-[3px] text-rs-caption">{roleLabels[role]}</span>
        {save && (
          <span
            role="status"
            className={`flex items-center gap-1.5 text-rs-caption ${trouble ? "rounded-rs-pill bg-rs-surface px-2 py-0.5 font-medium text-rs-danger-strong" : ""}`}
          >
            {saved && (
              <svg aria-hidden="true" viewBox="0 0 24 24" className="size-3.5">
                <path d="M5 12l5 5 9-10" fill="none" stroke="currentColor" strokeWidth={2.2} strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            )}
            {save.text}
            {save.detail && <span className="font-normal">· {save.detail}</span>}
          </span>
        )}
        {presence && <div className="ml-auto flex items-center">{presence}</div>}
      </div>
    </header>
  );
}
