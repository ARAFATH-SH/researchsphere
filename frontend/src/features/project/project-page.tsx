"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { type ReactNode, useCallback, useEffect, useId, useState } from "react";

import { Avatar } from "@/components/avatar";
import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { rsSelectClass } from "@/components/ui/field";
import { homeCard } from "@/features/home/cards";
import { Toast, toastDuration } from "@/features/profile/toast";
import { RecommendationList } from "@/features/recommendation/recommendation-list";
import { RecommendationsProvider } from "@/features/recommendation/recommendations-provider";
import type { Result } from "@/lib/api/call";

import {
  getProject,
  getRecommendedResearchers,
  type Project,
  type ProjectStatus,
  type ProjectUpdate,
  updateProject,
} from "./api";
import { StatusBadge, statusLabels } from "./badges";
import { DeleteProjectDialog } from "./delete-project-dialog";
import { ProjectForm, valuesOf } from "./project-form";
import { ReportProjectDialog } from "./report-project-dialog";
import type { ProjectValues } from "./validation";

type Load = { status: "loading" } | { status: "notFound" } | { status: "error" } | { status: "ok"; project: Project };

// ProjectPage is /projects/[projectId] (design/screens/ProjectDetail.html): the project's
// details (FR-10), its members and, for the owner, editing, the status, deleting
// (FR-10.2, FR-10.6) and the researchers who fit it (AI-2.3). Anyone who is not a member may
// report it (FR-15.11). A project the caller may not see is "not found", like one that does
// not exist. Join requests, invitations, ownership transfer, the workspace and venues come
// with their slices.
export function ProjectPage({ projectId }: { projectId: string }) {
  const [attempt, setAttempt] = useState(0);
  const key = `${projectId}#${attempt}`;
  const [result, setResult] = useState<{ key: string; value: Load } | null>(null);

  useEffect(() => {
    let current = true;
    void getProject(projectId).then((res) => {
      if (!current) return;
      const value: Load = res.ok
        ? { status: "ok", project: res.data }
        : res.code === "not_found"
          ? { status: "notFound" }
          : { status: "error" };
      setResult({ key, value });
    });
    return () => {
      current = false;
    };
  }, [projectId, key]);

  const setProject = useCallback(
    (project: Project) => setResult({ key, value: { status: "ok", project } }),
    [key],
  );

  const load: Load = result?.key === key ? result.value : { status: "loading" };
  switch (load.status) {
    case "loading":
      return (
        <p role="status" className="text-rs-body text-rs-text-muted">
          Loading the project…
        </p>
      );
    case "notFound":
      return (
        <StateCard title="Project not found" text="This project does not exist or is not available to you.">
          <Link href="/projects" className={rsButtonClass({ full: true })}>
            Go to My projects
          </Link>
        </StateCard>
      );
    case "error":
      return (
        <StateCard title="Something went wrong" text="We couldn’t load this project.">
          <button type="button" onClick={() => setAttempt((n) => n + 1)} className={rsButtonClass({ full: true })}>
            Try again
          </button>
        </StateCard>
      );
    case "ok":
      return <ProjectView project={load.project} onChange={setProject} />;
  }
}

function StateCard({ title, text, children }: { title: string; text: string; children: ReactNode }) {
  return (
    <div className={`${homeCard} mx-auto flex w-full max-w-[400px] flex-col gap-4 p-6 text-center`}>
      <h1 className="text-rs-h3 leading-rs-tight font-bold text-rs-text">{title}</h1>
      <p className="text-rs-body text-rs-text-muted">{text}</p>
      {children}
    </div>
  );
}

const roleText = { owner: "You are the owner", editor: "You are an editor", viewer: "You are a viewer" } as const;

type Open = "edit" | "delete" | "report" | null;

function ProjectView({ project, onChange }: { project: Project; onChange: (project: Project) => void }) {
  const router = useRouter();
  const [open, setOpen] = useState<Open>(null);
  const [editDirty, setEditDirty] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const isOwner = project.myRole === "owner";
  const isMember = project.myRole !== null;

  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(() => setToast(null), toastDuration);
    return () => clearTimeout(timer);
  }, [toast]);

  return (
    <div className="flex flex-wrap items-start gap-6 max-[640px]:gap-4">
      <div className="flex min-w-0 flex-[999_1_560px] flex-col gap-4">
        <section aria-label="Project summary" className={`${homeCard} overflow-hidden`}>
          <div className="flex flex-col gap-1.5 bg-rs-accent-strong p-5 text-rs-surface">
            {project.myRole && (
              <span className="self-start rounded-rs-pill bg-rs-surface px-2.5 py-0.5 text-rs-caption font-medium text-rs-accent-strong">
                {roleText[project.myRole]}
              </span>
            )}
            <h1 className="text-rs-h3 leading-rs-tight font-bold">{project.title}</h1>
            <p className="text-rs-body whitespace-pre-line">{project.description}</p>
          </div>
          {(isOwner || !isMember) && (
            <div className="flex flex-wrap items-center gap-2.5 border-b border-rs-divider px-5 py-4">
              {isOwner ? (
                <>
                  <button
                    id="edit-project"
                    type="button"
                    onClick={() => setOpen("edit")}
                    className={rsButtonClass({ variant: "outline", size: "md" })}
                  >
                    Edit details
                  </button>
                  <StatusSelect project={project} onChange={onChange} />
                </>
              ) : (
                <button
                  id="report-project"
                  type="button"
                  onClick={() => setOpen("report")}
                  className={`${rsButtonClass({ variant: "secondary", size: "md" })} ml-auto`}
                >
                  Report project
                </button>
              )}
            </div>
          )}
          <div className="flex flex-col gap-2.5 px-5 py-4">
            <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-rs-body text-rs-text">
              <span>
                Owner:{" "}
                <Link href={`/profile/${project.owner.userId}`} className="font-medium text-rs-accent-strong hover:underline">
                  {project.owner.fullName}
                </Link>
              </span>
              <span aria-hidden="true">·</span>
              <span>{project.memberCount === 1 ? "1 member" : `${project.memberCount} members`}</span>
              <StatusBadge status={project.status} />
            </p>
            {project.requiredSkills.length > 0 && (
              <>
                <h2 className="text-rs-body font-medium text-rs-text">Required skills</h2>
                <ul aria-label="Required skills" className="flex flex-wrap gap-1.5">
                  {project.requiredSkills.map((skill) => (
                    <li key={skill.id} className="rounded-rs-pill bg-rs-accent-soft px-2.5 py-1 text-rs-caption text-rs-heading">
                      {skill.name}
                    </li>
                  ))}
                </ul>
              </>
            )}
            {project.abstract !== null && (
              <>
                <h2 className="text-rs-body font-medium text-rs-text">Abstract</h2>
                <p className="text-rs-body leading-rs-relaxed whitespace-pre-line text-rs-text">{project.abstract}</p>
              </>
            )}
          </div>
        </section>

        <MembersCard project={project} />

        {isOwner && (
          <section
            aria-labelledby="delete-project-h"
            className="flex flex-col gap-3 rounded-card border border-rs-danger-line bg-rs-surface p-4 shadow-card"
          >
            <h2 id="delete-project-h" className="text-rs-lead leading-rs-snug font-medium text-rs-danger-strong">
              Delete this project
            </h2>
            <div className="flex flex-wrap items-center gap-2.5">
              <p className="min-w-[220px] flex-1 text-rs-body text-rs-text">
                Delete this project and all its files. Type the project title to confirm.
              </p>
              <button
                id="delete-project"
                type="button"
                onClick={() => setOpen("delete")}
                className={rsButtonClass({ variant: "destructive", size: "md" })}
              >
                Delete project
              </button>
            </div>
          </section>
        )}
      </div>

      {isOwner && (
        <aside
          aria-label="Project recommendations"
          className="flex min-w-0 flex-[1_1_300px] max-w-[340px] flex-col gap-3 max-[640px]:max-w-none"
        >
          <RecommendationsProvider load={() => getRecommendedResearchers(project.id)}>
            <RecommendationList limit={10} variant="project" />
          </RecommendationsProvider>
        </aside>
      )}

      {open === "edit" && (
        <Dialog title="Edit project" dirty={editDirty} onClose={() => setOpen(null)} returnFocusId="edit-project">
          <ProjectForm
            initial={valuesOf(project)}
            onDirtyChange={setEditDirty}
            onSubmit={(values) => saveChanges(project, values)}
            onDone={(next) => {
              onChange(next);
              setOpen(null);
              setEditDirty(false);
              setToast("Saved");
            }}
            actions={(busy) => (
              <div className="flex flex-wrap justify-end gap-2 pt-1">
                <button
                  type="button"
                  onClick={() => setOpen(null)}
                  className={rsButtonClass({ variant: "secondary", size: "form" })}
                >
                  Cancel
                </button>
                <button type="submit" disabled={busy} className={rsButtonClass({ size: "form" })}>
                  Save
                </button>
              </div>
            )}
          />
        </Dialog>
      )}
      {open === "delete" && (
        <DeleteProjectDialog
          projectId={project.id}
          title={project.title}
          onClose={() => setOpen(null)}
          onDeleted={() => router.push("/projects")}
          returnFocusId="delete-project"
        />
      )}
      {open === "report" && (
        <ReportProjectDialog projectId={project.id} onClose={() => setOpen(null)} returnFocusId="report-project" />
      )}
      {toast && <Toast message={toast} />}
    </div>
  );
}

// saveChanges sends only the fields that changed; an emptied abstract is cleared (null). With
// nothing changed it answers with the project as it is.
export async function saveChanges(project: Project, values: ProjectValues): Promise<Result<Project>> {
  const before = valuesOf(project);
  const body: ProjectUpdate = {};
  const title = values.title.trim();
  const description = values.description.trim();
  const abstract = values.abstract.trim();
  if (title !== before.title) body.title = title;
  if (description !== before.description) body.description = description;
  if (values.requiredSkills.join("\n") !== before.requiredSkills.join("\n")) body.requiredSkills = values.requiredSkills;
  if (abstract !== before.abstract.trim()) body.abstract = abstract === "" ? null : abstract;
  if (Object.keys(body).length === 0) return { ok: true, data: project };
  return updateProject(project.id, body);
}

const statusOptions: ProjectStatus[] = ["open", "closed", "archived"];

// StatusSelect lets the owner move the project between open, closed and archived (FR-10.2).
// The change is sent at once; if it fails, the old status comes back with a message.
function StatusSelect({ project, onChange }: { project: Project; onChange: (project: Project) => void }) {
  const id = useId();
  const [pending, setPending] = useState<ProjectStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const shown = pending ?? project.status;

  async function change(status: ProjectStatus) {
    if (status === project.status || pending) return;
    setPending(status);
    setError(null);
    const res = await updateProject(project.id, { status });
    setPending(null);
    if (res.ok) onChange(res.data);
    else setError("We couldn’t change the status. Try again.");
  }

  return (
    <div className="ml-auto flex flex-col items-end gap-1 max-[640px]:ml-0 max-[640px]:items-start">
      <div className="flex items-center gap-2">
        <label htmlFor={id} className="text-rs-body font-medium text-rs-text">
          Status
        </label>
        <select
          id={id}
          value={shown}
          disabled={pending !== null}
          onChange={(e) => void change(e.target.value as ProjectStatus)}
          className={`${rsSelectClass} w-auto`}
        >
          {statusOptions.map((s) => (
            <option key={s} value={s}>
              {statusLabels[s]}
            </option>
          ))}
        </select>
      </div>
      {error ? (
        <p role="alert" className="text-rs-caption text-rs-danger">
          {error}
        </p>
      ) : (
        <p className="text-rs-caption text-rs-text-muted">
          {shown === "open"
            ? "Found in search."
            : shown === "closed"
              ? "Not found in search; the page stays visible."
              : "Only members can see it."}
        </p>
      )}
    </div>
  );
}

// MembersCard lists the members. Until membership (slice 20) the owner is the only one.
function MembersCard({ project }: { project: Project }) {
  const owner = project.owner;
  return (
    <section aria-labelledby="members-h" className={homeCard}>
      <div className="border-b border-rs-divider px-4 py-3.5">
        <h2 id="members-h" className="text-rs-lead leading-rs-snug font-medium text-rs-text">
          Members ({project.memberCount})
        </h2>
      </div>
      <ul aria-label="Members">
        <li className="flex flex-wrap items-center gap-3 px-4 py-3">
          <Avatar name={owner.fullName} src={owner.photoUrl} size="post" decorative />
          <span className="flex min-w-[180px] flex-1 flex-col">
            <Link href={`/profile/${owner.userId}`} className="text-rs-body font-bold text-rs-text no-underline hover:underline">
              {owner.fullName}
            </Link>
            {owner.headline && <span className="text-rs-caption text-rs-text-muted">{owner.headline}</span>}
          </span>
          <span className="rounded-rs-pill bg-rs-accent-strong px-3 py-1 text-rs-caption font-medium text-rs-surface">
            Owner
          </span>
        </li>
      </ul>
    </section>
  );
}
