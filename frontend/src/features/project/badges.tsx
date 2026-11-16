import type { ProjectRole, ProjectStatus } from "./api";

export const statusLabels: Record<ProjectStatus, string> = {
  open: "Open",
  closed: "Closed",
  archived: "Archived",
};

export const roleLabels: Record<ProjectRole, string> = {
  owner: "Owner",
  editor: "Editor",
  viewer: "Viewer",
};

const pill = "inline-block whitespace-nowrap rounded-rs-pill px-2.5 py-0.5 text-rs-caption font-medium";

// StatusBadge is a project's status as the design draws it in lists: an outlined pill, or a
// soft one for an open project in search results (soft).
export function StatusBadge({ status, soft = false }: { status: ProjectStatus; soft?: boolean }) {
  const look = soft ? "bg-rs-accent-soft text-rs-heading" : "border border-rs-control text-rs-text";
  return <span className={`${pill} ${look}`}>{statusLabels[status]}</span>;
}

// RoleBadge is the caller's role in a project: filled for the owner, soft for the others.
export function RoleBadge({ role }: { role: ProjectRole }) {
  const look = role === "owner" ? "bg-rs-accent-strong text-rs-surface" : "bg-rs-accent-soft text-rs-heading";
  return <span className={`${pill} ${look}`}>{roleLabels[role]}</span>;
}

// ProjectIcon is the folder of the project rows.
export function ProjectIcon({ className = "size-6" }: { className?: string }) {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinejoin="round"
      className={className}
    >
      <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
    </svg>
  );
}
