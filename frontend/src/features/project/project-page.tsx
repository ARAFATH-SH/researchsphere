"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { type ReactNode, useCallback, useEffect, useId, useRef, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { rsSelectClass } from "@/components/ui/field";
import { homeCard } from "@/features/home/cards";
import { joinRequestsAnchor } from "@/features/notification/types";
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
import { InviteControl } from "./membership/invite-button";
import { JoinActions } from "./membership/join-actions";
import { MembersCard } from "./membership/members-card";
import { OwnershipCard } from "./membership/ownership-card";
import { InvitationsCard, JoinRequestsCard } from "./membership/requests-card";
import { useMembership } from "./membership/use-membership";
import { ProjectForm, valuesOf } from "./project-form";
import { ReportProjectDialog } from "./report-project-dialog";
import type { ProjectValues } from "./validation";

type Load = { status: "loading" } | { status: "notFound" } | { status: "error" } | { status: "ok"; project: Project };

// ProjectPage is /projects/[projectId] (design/screens/ProjectDetail.html): the project's
// details (FR-10), its members (FR-11) and, for the owner, editing, the status, join requests,
// invitations, roles, transferring and deleting (FR-10.2, FR-10.5, FR-10.6, FR-11.2 to 11.4)
// and the researchers who fit it, to invite (AI-2.3). A non-member may ask to join or answer an
// invitation (FR-11.1, FR-11.3) and report it (FR-15.11); a member may leave (FR-11.5). A
// project the caller may not see is "not found", like one that does not exist. The workspace
// and venues come with their slices.
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

  // refresh reads the project again without the loading state, after a membership change. A
  // project the caller can no longer see (they left an archived one) becomes "not found"; any
  // other failure keeps the page as it is.
  const refresh = useCallback(() => {
    void getProject(projectId).then((res) => {
      if (res.ok) setResult({ key, value: { status: "ok", project: res.data } });
      else if (res.code === "not_found") setResult({ key, value: { status: "notFound" } });
    });
  }, [projectId, key]);

  // The toast lives here so it outlasts a change of role, which rebuilds the view.
  const [toast, setToast] = useState<string | null>(null);
  const showToast = useCallback((message: string) => setToast(message), []);
  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(() => setToast(null), toastDuration);
    return () => clearTimeout(timer);
  }, [toast]);

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
      return (
        <>
          <ProjectView project={load.project} onChange={setProject} refresh={refresh} showToast={showToast} />
          {toast && <Toast message={toast} />}
        </>
      );
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

type Open = "edit" | "report" | null;

function ProjectView({
  project,
  onChange,
  refresh,
  showToast,
}: {
  project: Project;
  onChange: (project: Project) => void;
  refresh: () => void;
  showToast: (message: string) => void;
}) {
  const router = useRouter();
  const [open, setOpen] = useState<Open>(null);
  const [editDirty, setEditDirty] = useState(false);
  const isOwner = project.myRole === "owner";
  const isMember = project.myRole !== null;

  const membership = useMembership({ project, refreshProject: refresh, showToast });
  const shared = { membership, refreshProject: refresh, showToast };

  // A join request notification opens the page at the Join requests card; it is there once
  // the requests have loaded.
  const scrolled = useRef(false);
  const requestsStatus = membership.requests.value.status;
  useEffect(() => {
    if (scrolled.current || requestsStatus !== "ok" || window.location.hash !== `#${joinRequestsAnchor}`) return;
    scrolled.current = true;
    document.getElementById(joinRequestsAnchor)?.scrollIntoView?.({ block: "start" });
  }, [requestsStatus]);

  const view = (
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
                <>
                  <JoinActions project={project} {...shared} />
                  <button
                    id="report-project"
                    type="button"
                    onClick={() => setOpen("report")}
                    className={`${rsButtonClass({ variant: "secondary", size: "md" })} ml-auto`}
                  >
                    Report project
                  </button>
                </>
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

        {isOwner && <JoinRequestsCard projectId={project.id} {...shared} />}
        <MembersCard project={project} {...shared} />
        {isOwner && <InvitationsCard projectId={project.id} membership={membership} showToast={showToast} />}
        {isOwner && (
          <OwnershipCard
            project={project}
            membership={membership}
            showToast={showToast}
            onTransferred={onChange}
            onDeleted={() => router.push("/projects")}
          />
        )}
      </div>

      {isOwner && (
        <aside
          aria-label="Project recommendations"
          className="flex min-w-0 flex-[1_1_300px] max-w-[340px] flex-col gap-3 max-[640px]:max-w-none"
        >
          <RecommendationList
            limit={10}
            variant="project"
            action={(item) => (
              <InviteControl
                person={{ userId: item.userId, fullName: item.fullName, headline: item.headline, photoUrl: item.photoUrl }}
                state={membership.inviteState(item.userId)}
                onInvite={membership.invite}
              />
            )}
          />
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
              showToast("Saved");
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
      {open === "report" && (
        <ReportProjectDialog projectId={project.id} onClose={() => setOpen(null)} returnFocusId="report-project" />
      )}
    </div>
  );

  // The owner's recommendations feed both the side card and the invite dialog's quick picks.
  return isOwner ? (
    <RecommendationsProvider load={() => getRecommendedResearchers(project.id)}>{view}</RecommendationsProvider>
  ) : (
    view
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
