import Link from "next/link";

import { emptyCard, homeCard } from "@/features/home/cards";
import type { ProjectSearchPage, ProjectSearchResult } from "@/features/project/api";
import { StatusBadge } from "@/features/project/badges";

import type { SearchState } from "./search-params";
import { lastPage, Pages } from "./search-results";

// ProjectResults is the card of one page of open projects (FR-10.3) on the Projects tab of
// design/screens/Search.html: a count line, a row per project linking to its page, and the
// page links.
export function ProjectResults({ search, result }: { search: SearchState; result: ProjectSearchPage }) {
  const last = lastPage(result);
  const projects = result.total === 1 ? "1 open project" : `${result.total} open projects`;
  return (
    <div className={homeCard}>
      <p className="border-b border-rs-divider px-4 py-3 text-rs-body text-rs-text-muted">
        {projects} · page {result.page} of {last}
      </p>
      <ul aria-label="Projects">
        {result.items.map((project) => (
          <ProjectRow key={project.id} project={project} />
        ))}
      </ul>
      {last > 1 && <Pages search={search} page={result.page} last={last} />}
    </div>
  );
}

function ProjectRow({ project }: { project: ProjectSearchResult }) {
  const needs = project.requiredSkills.length > 0 ? `Needs: ${project.requiredSkills.join(" · ")} · ` : "";
  return (
    <li className="flex flex-col gap-1 border-b border-rs-divider px-4 py-3.5">
      <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <Link
          href={`/projects/${project.id}`}
          className="rounded-rs-2xs text-rs-lead font-bold text-rs-text no-underline hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none"
        >
          {project.title}
        </Link>
        <StatusBadge status={project.status} soft />
        {project.matchedOn === "meaning" && (
          <span className="rounded-full bg-rs-accent-soft px-2.5 py-0.5 text-rs-caption font-medium text-rs-heading">
            Related by meaning
          </span>
        )}
      </span>
      <span className="line-clamp-2 text-rs-body text-rs-text">{project.description}</span>
      <span className="text-rs-caption text-rs-text-muted">
        {needs}Owner: {project.owner.fullName}
      </span>
    </li>
  );
}

// ProjectNoResults is the Projects tab's empty state. A search by meaning suggests other
// words; a keyword search, another spelling.
export function ProjectNoResults({ q, mode }: { q: string; mode: ProjectSearchPage["mode"] }) {
  return (
    <div className={`${emptyCard} flex flex-col items-start gap-1.5 px-4 py-3.5`}>
      <h2 className="text-rs-lead leading-rs-snug font-medium text-rs-text">
        {mode === "hybrid" ? "No open projects found." : `No open projects match “${q}”.`}
      </h2>
      <p className="text-rs-body text-rs-text-muted">
        {mode === "hybrid"
          ? "Try describing the research in different words."
          : "Try a different spelling, a skill, or search by meaning."}
      </p>
    </div>
  );
}
