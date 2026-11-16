"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useId, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { FormError } from "@/components/ui/field";
import { PageNav } from "@/components/ui/page-nav";
import { homeCard } from "@/features/home/cards";
import { relativeTime } from "@/features/notification/format";
import type { Result } from "@/lib/api/call";

import { createProject, listMyProjects, myProjectsPageSize, type ProjectPage, type ProjectSummary } from "./api";
import { ProjectIcon, RoleBadge, StatusBadge } from "./badges";
import { emptyProject, ProjectForm } from "./project-form";

// createTitleId is the Create card's title field, which "New project" focuses.
const createCardId = "create-project";

// ProjectsPage is /projects (design/screens/Projects.html): the caller's projects with
// their role and status (FR-11.6), and the Create a project card (FR-10.1) beside them, or
// under them on a phone. Invitations come with membership (slice 20).
export function ProjectsPage() {
  return (
    <div className="flex flex-wrap items-start gap-6 max-[640px]:gap-4">
      <div className="flex min-w-0 flex-[999_1_520px] flex-col gap-4">
        <MyProjects />
      </div>
      <CreateProjectCard />
    </div>
  );
}

// MyProjects lists one page of the caller's projects, most recently changed first.
function MyProjects() {
  const [page, setPage] = useState(1);
  const [attempt, setAttempt] = useState(0);
  const key = `${page}#${attempt}`;
  const [result, setResult] = useState<{ key: string; value: Result<ProjectPage> } | null>(null);

  useEffect(() => {
    let current = true;
    void listMyProjects(page).then((value) => {
      if (current) setResult({ key, value });
    });
    return () => {
      current = false;
    };
  }, [page, key]);

  const value = result?.key === key ? result.value : null;

  function focusCreate() {
    const card = document.getElementById(createCardId);
    card?.scrollIntoView?.({ block: "start" });
    card?.querySelector<HTMLInputElement>("input")?.focus();
  }

  let body;
  if (!value) {
    body = (
      <p role="status" className="px-4 py-3.5 text-rs-body text-rs-text-muted">
        Loading your projects…
      </p>
    );
  } else if (!value.ok) {
    body = (
      <div className="flex flex-col items-start gap-3 px-4 py-3.5">
        <FormError look="rs">We couldn’t load your projects.</FormError>
        <button
          type="button"
          onClick={() => setAttempt((n) => n + 1)}
          className={rsButtonClass({ variant: "secondary", size: "sm" })}
        >
          Try again
        </button>
      </div>
    );
  } else if (value.data.total === 0) {
    body = (
      <p className="px-4 py-3.5 text-rs-body text-rs-text">
        You have no projects yet. Create one to find collaborators for it.
      </p>
    );
  } else {
    body = (
      <>
        <ul aria-label="My projects">
          {value.data.items.map((project) => (
            <li key={project.id} className="border-b border-rs-divider last:border-b-0">
              <MyProjectRow project={project} />
            </li>
          ))}
        </ul>
        {value.data.total > myProjectsPageSize && (
          <PageNav page={page} pageSize={myProjectsPageSize} total={value.data.total} onPage={setPage} />
        )}
      </>
    );
  }

  return (
    <section aria-labelledby="my-projects-h" aria-busy={!value} className={homeCard}>
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-rs-divider px-4 py-3.5">
        <h1 id="my-projects-h" className="text-rs-h3 leading-rs-tight font-bold text-rs-text">
          My projects
        </h1>
        <button type="button" onClick={focusCreate} className={rsButtonClass({ size: "sm" })}>
          New project
        </button>
      </div>
      {body}
    </section>
  );
}

// editedText is "edited 3 h ago", "edited yesterday" or "edited 3 Oct 2026".
export function editedText(iso: string): string {
  const when = relativeTime(iso);
  return `edited ${when.charAt(0).toLowerCase()}${when.slice(1)}`;
}

function MyProjectRow({ project }: { project: ProjectSummary }) {
  const meta =
    project.myRole === "owner" ? editedText(project.updatedAt) : `Owner: ${project.owner.fullName} · ${editedText(project.updatedAt)}`;
  return (
    <Link
      href={`/projects/${project.id}`}
      className="flex flex-wrap items-start gap-3 px-4 py-3.5 text-rs-text no-underline hover:bg-rs-subtle focus-visible:shadow-focus focus-visible:outline-none"
    >
      <span className="flex size-12 flex-none items-center justify-center rounded-rs-md bg-rs-accent-soft text-rs-heading">
        <ProjectIcon />
      </span>
      <span className="flex min-w-[220px] flex-1 flex-col gap-0.5">
        <strong className="text-rs-lead font-bold">{project.title}</strong>
        <span className="line-clamp-2 text-rs-body">{project.description}</span>
        <span className="text-rs-caption text-rs-text-muted">{meta}</span>
      </span>
      <span className="flex items-center gap-1.5">
        {project.myRole && <RoleBadge role={project.myRole} />}
        <StatusBadge status={project.status} />
      </span>
    </Link>
  );
}

// CreateProjectCard is the design's Create a project card (FR-10.1). A new project opens
// its page.
function CreateProjectCard() {
  const router = useRouter();
  const headingId = useId();
  return (
    <section
      id={createCardId}
      aria-labelledby={headingId}
      className={`${homeCard} min-w-0 flex-[1_1_360px] max-w-[420px] overflow-hidden max-[640px]:max-w-none`}
    >
      <div className="bg-rs-accent-strong px-4 py-3.5 text-rs-surface">
        <h2 id={headingId} className="text-rs-lead leading-rs-snug font-medium">
          Create a project
        </h2>
      </div>
      <div className="p-4">
        <ProjectForm
          initial={emptyProject}
          onSubmit={(v) =>
            createProject({
              title: v.title.trim(),
              description: v.description.trim(),
              requiredSkills: v.requiredSkills,
              ...(v.abstract.trim() ? { abstract: v.abstract.trim() } : {}),
            })
          }
          onDone={(project) => router.push(`/projects/${project.id}`)}
          note={
            <p className="rounded-rs-md bg-rs-accent-soft px-3 py-2.5 text-rs-caption text-rs-heading">
              You’ll be the owner. Only members can read the abstract.
            </p>
          }
          actions={(busy) => (
            <button type="submit" disabled={busy} className={rsButtonClass({ full: true })}>
              Create project
            </button>
          )}
        />
      </div>
    </section>
  );
}
