"use client";

import Link from "next/link";
import { type ReactNode, useId, useState } from "react";

import { Card } from "@/components/ui/card";

import type { Education, Profile, Tag } from "./api";
import { EditButton, editButtonId, type EditSection, editHref } from "./edit-button";
import { yearRange } from "./format";

// EditControls are given only on the owner's profile: open shows a section's dialog.
export type EditControls = { open: (section: EditSection) => void };

// The main column's sections, one card each. On the owner's profile each has a pencil
// and an empty section shows a prompt to fill it in; on anyone else's an empty section is
// hidden, except Publications.
export function ProfileSections({ profile, edit }: { profile: Profile; edit?: EditControls }) {
  return (
    <>
      <Section title="About" section="about" empty={!profile.bio} edit={edit} prompt="Write a short bio">
        {profile.bio && <Bio text={profile.bio} />}
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
      <Section title="Skills" section="skills" empty={profile.skills.length === 0} edit={edit} prompt="Add your skills">
        <Chips label="Skills" tags={profile.skills} chipClass="border border-border bg-background text-text" />
      </Section>
      <Section
        title="Education"
        section="education"
        empty={profile.education.length === 0}
        edit={edit}
        prompt="Add your education"
      >
        <EducationList entries={profile.education} />
      </Section>
      {/* Confirmed publications arrive with slice 11 (FR-5.5). */}
      <SectionCard title="Publications">
        <p className="text-sm text-text-muted">No publications yet.</p>
      </SectionCard>
    </>
  );
}

type SectionProps = {
  title: string;
  section: EditSection;
  empty: boolean;
  edit?: EditControls;
  prompt: string;
  children: ReactNode;
};

function Section({ title, section, empty, edit, prompt, children }: SectionProps) {
  if (empty && !edit) return null;
  const action = edit && (
    <EditButton id={editButtonId(section)} label={`Edit ${title.toLowerCase()}`} onClick={() => edit.open(section)} />
  );
  return (
    <SectionCard title={title} action={action}>
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
// end of the title row.
export function SectionCard({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  const headingId = useId();
  return (
    <Card className="p-4 md:p-6">
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
