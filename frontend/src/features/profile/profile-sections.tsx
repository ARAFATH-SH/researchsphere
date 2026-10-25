"use client";

import Link from "next/link";
import { type ReactNode, useId, useState } from "react";

import { Card } from "@/components/ui/card";

import type { Education, Experience, Profile, Tag } from "./api";
import {
  AddButton,
  BriefcaseIcon,
  EditButton,
  editButtonId,
  type EditSection,
  editHref,
  experienceButtonId,
} from "./edit-button";
import { employmentTypeLabels, experienceDates, locationTypeLabels, yearRange } from "./format";

// EditControls are given only on the owner's profile: open shows a section's dialog
// ("experience" adds an entry), and editExperience opens one experience entry.
export type EditControls = { open: (section: EditSection) => void; editExperience: (entry: Experience) => void };

// The first entries a section shows before its "Show all" row, and how many skills the
// About card names.
const shownExperience = 3;
const shownEducation = 2;
const shownSkills = 6;
const topSkills = 5;

// skillsSectionId is the Skills card's id, which "Top skills" links to.
const skillsSectionId = "skills";

// The main column's sections, one card each. On the owner's profile each has a pencil
// and an empty section shows a prompt to fill it in; on anyone else's an empty section is
// hidden, except Publications.
export function ProfileSections({ profile, edit }: { profile: Profile; edit?: EditControls }) {
  return (
    <>
      <Section title="About" section="about" empty={!profile.bio} edit={edit} prompt="Write a short bio">
        {profile.bio && (
          <>
            <Bio text={profile.bio} />
            <TopSkills skills={profile.skills} />
          </>
        )}
      </Section>
      <Section
        title="Research interests"
        section="interests"
        empty={profile.interests.length === 0}
        edit={edit}
        prompt="Add your research interests"
      >
        <Chips label="Research interests" tags={profile.interests} chipClass="bg-primary-soft text-primary" />
      </Section>
      <Section
        id={skillsSectionId}
        title="Skills"
        section="skills"
        empty={profile.skills.length === 0}
        edit={edit}
        prompt="Add your skills"
      >
        <ShowAll total={profile.skills.length} shown={shownSkills}>
          {(limit) => (
            <Chips
              label="Skills"
              tags={profile.skills.slice(0, limit)}
              chipClass="border border-border bg-background text-text"
            />
          )}
        </ShowAll>
      </Section>
      <ExperienceSection entries={profile.experience} edit={edit} />
      <Section
        title="Education"
        section="education"
        empty={profile.education.length === 0}
        edit={edit}
        prompt="Add your education"
      >
        <ShowAll total={profile.education.length} shown={shownEducation}>
          {(limit) => <EducationList entries={profile.education.slice(0, limit)} />}
        </ShowAll>
      </Section>
      {/* Confirmed publications arrive with slice 11 (FR-5.5). */}
      <SectionCard title="Publications">
        <p className="text-sm text-text-muted">No publications yet.</p>
      </SectionCard>
    </>
  );
}

type SectionProps = {
  id?: string;
  title: string;
  section: EditSection;
  empty: boolean;
  edit?: EditControls;
  prompt: string;
  children: ReactNode;
};

function Section({ id, title, section, empty, edit, prompt, children }: SectionProps) {
  if (empty && !edit) return null;
  const action = edit && (
    <EditButton id={editButtonId(section)} label={`Edit ${title.toLowerCase()}`} onClick={() => edit.open(section)} />
  );
  return (
    <SectionCard id={id} title={title} action={action}>
      {empty ? (
        <Link href={editHref(section)} className="text-sm text-secondary hover:underline">
          {prompt}
        </Link>
      ) : (
        children
      )}
    </SectionCard>
  );
}

// SectionCard is a card with a titled section; action, such as a pencil, sits at the
// end of the title row. id lets a link scroll to the card, clear of the fixed top bar and
// the sticky profile bar.
type SectionCardProps = { id?: string; title: string; action?: ReactNode; children: ReactNode };

export function SectionCard({ id, title, action, children }: SectionCardProps) {
  const headingId = useId();
  return (
    <Card id={id} className="scroll-mt-32 p-4 md:p-6">
      <section aria-labelledby={headingId}>
        <div className="-my-2 flex items-center justify-between gap-4">
          <h2 id={headingId} className="py-2 text-lg font-semibold text-text">
            {title}
          </h2>
          {action}
        </div>
        <div className="mt-3">{children}</div>
      </section>
    </Card>
  );
}

// A bio longer than about six lines starts collapsed. The text cannot be measured
// before it is drawn, so the line and character counts stand in for its height.
const collapsedLines = 6;
const collapsedChars = 600;

function Bio({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  const textId = useId();
  const long = text.split("\n").length > collapsedLines || text.length > collapsedChars;
  return (
    <>
      <p
        id={textId}
        className={`max-w-text text-base whitespace-pre-line text-text ${long && !expanded ? "line-clamp-6" : ""}`}
      >
        {text}
      </p>
      {long && (
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls={textId}
          onClick={() => setExpanded(!expanded)}
          className="mt-2 rounded-sm text-sm font-semibold text-secondary hover:underline focus-visible:ring-2 focus-visible:ring-primary focus-visible:outline-none"
        >
          {expanded ? "Show less" : "Show more"}
        </button>
      )}
    </>
  );
}

// TopSkills names the first skills under the bio and links to the Skills card.
function TopSkills({ skills }: { skills: Tag[] }) {
  if (skills.length === 0) return null;
  return (
    <a
      href={`#${skillsSectionId}`}
      className="mt-4 flex items-center gap-4 rounded-lg border border-border p-4 hover:bg-background focus-visible:ring-2 focus-visible:ring-primary focus-visible:outline-none"
    >
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-semibold text-text">Top skills</span>
        <span className="block text-sm text-text">
          {skills
            .slice(0, topSkills)
            .map((t) => t.name)
            .join(" • ")}
        </span>
      </span>
      <span aria-hidden="true" className="text-text-muted">
        →
      </span>
    </a>
  );
}

// ShowAll renders the first `shown` entries of a list and, when there are more, a
// full-width "Show all N" row at the bottom of the card that expands the list in place.
// noun, when given, names the entries ("Show all 4 experiences").
type ShowAllProps = { total: number; shown: number; noun?: string; children: (limit: number) => ReactNode };

function ShowAll({ total, shown, noun, children }: ShowAllProps) {
  const [expanded, setExpanded] = useState(false);
  const listId = useId();
  const collapsible = total > shown;
  return (
    <>
      <div id={listId}>{children(collapsible && !expanded ? shown : total)}</div>
      {collapsible && !expanded && (
        <button
          type="button"
          aria-expanded={false}
          aria-controls={listId}
          onClick={() => setExpanded(true)}
          className="-mx-4 mt-4 -mb-4 flex w-[calc(100%+2rem)] items-center justify-center gap-2 rounded-b-lg border-t border-border py-3 text-sm font-semibold text-text-muted hover:bg-background hover:text-text focus-visible:ring-2 focus-visible:ring-primary focus-visible:outline-none focus-visible:ring-inset md:-mx-6 md:-mb-6 md:w-[calc(100%+3rem)]"
        >
          Show all {total}
          {noun && ` ${noun}`} <span aria-hidden="true">→</span>
        </button>
      )}
    </>
  );
}

function Chips({ label, tags, chipClass }: { label: string; tags: Tag[]; chipClass: string }) {
  return (
    <ul aria-label={label} className="flex flex-wrap gap-2">
      {tags.map((t) => (
        <li key={t.id} className={`rounded-full px-3 py-1 text-sm font-medium ${chipClass}`}>
          {t.name}
        </li>
      ))}
    </ul>
  );
}

function EducationList({ entries }: { entries: Education[] }) {
  return (
    <ul aria-label="Education" className="divide-y divide-border">
      {entries.map((e) => {
        const study = [e.degree, e.fieldOfStudy].filter(Boolean).join(", ");
        return (
          <li key={e.id} className="py-3 first:pt-0 last:pb-0">
            <p className="font-semibold text-text">{e.institution}</p>
            {study && <p className="text-sm text-text">{study}</p>}
            <p className="text-sm text-text-muted">{yearRange(e.startYear, e.endYear)}</p>
          </li>
        );
      })}
    </ul>
  );
}

// ExperienceSection lists work experience in the order the API returns it (FR-4.8). On
// the owner's profile a "+" adds an entry and each entry has a pencil; an empty section
// then prompts for one. On anyone else's an empty section is hidden.
function ExperienceSection({ entries, edit }: { entries: Experience[]; edit?: EditControls }) {
  if (entries.length === 0 && !edit) return null;
  const action = edit && (
    <AddButton id={editButtonId("experience")} label="Add experience" onClick={() => edit.open("experience")} />
  );
  return (
    <SectionCard title="Experience" action={action}>
      {entries.length === 0 ? (
        <Link href={editHref("experience")} className="text-sm text-secondary hover:underline">
          Add experience
        </Link>
      ) : (
        <ShowAll total={entries.length} shown={shownExperience} noun="experiences">
          {(limit) => (
            <ul aria-label="Experience" className="divide-y divide-border">
              {entries.slice(0, limit).map((e) => (
                <ExperienceEntry key={e.id} entry={e} edit={edit} />
              ))}
            </ul>
          )}
        </ShowAll>
      )}
    </SectionCard>
  );
}

function ExperienceEntry({ entry: e, edit }: { entry: Experience; edit?: EditControls }) {
  const place = [e.location, e.locationType && locationTypeLabels[e.locationType]].filter(Boolean).join(" · ");
  return (
    <li className="flex items-start gap-3 py-4 first:pt-0 last:pb-0">
      <span aria-hidden="true" className="flex size-12 shrink-0 items-center justify-center rounded-md bg-primary-soft text-primary">
        <BriefcaseIcon />
      </span>
      <div className="min-w-0 flex-1">
        <p className="font-semibold text-text">{e.title}</p>
        <p className="text-sm text-text">
          {e.organization} · {employmentTypeLabels[e.employmentType]}
        </p>
        <p className="text-sm text-text-muted">{experienceDates(e)}</p>
        {place && <p className="text-sm text-text-muted">{place}</p>}
        {e.description && <ClampedText text={e.description} />}
      </div>
      {edit && (
        <EditButton
          id={experienceButtonId(e.id)}
          label={`Edit ${e.title} at ${e.organization}`}
          onClick={() => edit.editExperience(e)}
        />
      )}
    </li>
  );
}

// A description longer than about three lines starts clamped, as the bio does with six;
// the line and character counts stand in for its height.
const clampedLines = 3;
const clampedChars = 250;

function ClampedText({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  const textId = useId();
  const long = text.split("\n").length > clampedLines || text.length > clampedChars;
  return (
    <div className="mt-2">
      <p id={textId} className={`text-sm whitespace-pre-line text-text ${long && !expanded ? "line-clamp-3" : ""}`}>
        {text}
      </p>
      {long && (
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls={textId}
          onClick={() => setExpanded(!expanded)}
          className="rounded-sm text-sm font-semibold text-text-muted hover:text-secondary hover:underline focus-visible:ring-2 focus-visible:ring-primary focus-visible:outline-none"
        >
          {expanded ? "see less" : "…see more"}
        </button>
      )}
    </div>
  );
}
