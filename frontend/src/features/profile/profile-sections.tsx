"use client";

import Link from "next/link";
import { type ReactNode, useId, useState } from "react";

import { Card } from "@/components/ui/card";

import type { Education, Profile, Tag } from "./api";
import { EditButton, editButtonId, type EditSection, editHref } from "./edit-button";
import { yearRange } from "./format";

// EditControls are given only on the owner's profile: open shows a section's dialog.
export type EditControls = { open: (section: EditSection) => void };

// The first entries a section shows before its "Show all" row, and how many skills the
// About card names.
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
function ShowAll({ total, shown, children }: { total: number; shown: number; children: (limit: number) => ReactNode }) {
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
          Show all {total} <span aria-hidden="true">→</span>
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
