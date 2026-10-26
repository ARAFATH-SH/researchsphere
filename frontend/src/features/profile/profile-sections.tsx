"use client";

import Link from "next/link";
import { type ReactNode, useId, useState } from "react";

import { rsButtonClass } from "@/components/ui/button";
import { emptyCard, homeCard } from "@/features/home/cards";

import type { Education, Experience, Language, Profile, Tag } from "./api";
import {
  AddButton,
  EditButton,
  editButtonId,
  type EditSection,
  editHref,
  experienceButtonId,
  languageButtonId,
} from "./edit-button";
import {
  employmentTypeLabels,
  experienceDates,
  initials,
  locationTypeLabels,
  proficiencyLabels,
  yearRange,
} from "./format";

// EditControls are given only on the owner's profile: open shows a section's dialog
// ("experience" and "languages" add an entry), editExperience opens one experience entry
// and editLanguage one language.
export type EditControls = {
  open: (section: EditSection) => void;
  editExperience: (entry: Experience) => void;
  editLanguage: (entry: Language) => void;
};

// The first entries a section shows before its "Show all" row.
const shownExperience = 3;
const shownLanguages = 3;
const shownEducation = 2;
const shownSkills = 6;

// The main column's sections in the order of design/screens/Profile.html, one card each.
// Publications (slice 11) and Activity (FR-8.6) are not built yet and are left out. On the
// owner's profile each section has a pencil or "+" and an empty one shows the dashed
// empty-state card with a button to fill it in; on anyone else's an empty section is hidden.
export function ProfileSections({
  profile,
  edit,
}: {
  profile: Profile;
  edit?: EditControls;
}) {
  return (
    <>
      <Section
        title="About"
        section="about"
        empty={!profile.bio}
        edit={edit}
        prompt="Write a short bio"
      >
        {profile.bio && <Bio text={profile.bio} />}
      </Section>
      <TagsSection profile={profile} edit={edit} />
      <ExperienceSection entries={profile.experience} edit={edit} />
      <Section
        title="Education"
        section="education"
        empty={profile.education.length === 0}
        edit={edit}
        prompt="Add your education"
      >
        <ShowAll total={profile.education.length} shown={shownEducation}>
          {(limit) => (
            <EducationList entries={profile.education.slice(0, limit)} />
          )}
        </ShowAll>
      </Section>
      <LanguagesSection entries={profile.languages} edit={edit} />
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

function Section({
  title,
  section,
  empty,
  edit,
  prompt,
  children,
}: SectionProps) {
  if (empty && !edit) return null;
  const action = edit && (
    <EditButton
      id={editButtonId(section)}
      label={`Edit ${title.toLowerCase()}`}
      onClick={() => edit.open(section)}
    />
  );
  return (
    <SectionCard title={title} action={action} empty={empty}>
      {empty ? <EmptyPrompt section={section} label={prompt} /> : children}
    </SectionCard>
  );
}

// EmptyPrompt is the empty-state button of a section on the owner's profile. It links to
// /profile?edit=<section>, which opens the section's dialog.
function EmptyPrompt({
  section,
  label,
}: {
  section: EditSection;
  label: string;
}) {
  return (
    <Link
      href={editHref(section)}
      className={`${rsButtonClass({ variant: "outline", size: "sm" })} self-start`}
    >
      {label}
    </Link>
  );
}

// SectionCard is a card with a titled section (design/screens/Profile.html): 10px radius,
// 20px padding (16px in the aside), a 16px heading and the action, such as a pencil, at the
// end of the title row. empty draws the dashed empty-state card.
type SectionCardProps = {
  title: string;
  action?: ReactNode;
  empty?: boolean;
  aside?: boolean;
  children: ReactNode;
};

export function SectionCard({
  title,
  action,
  empty = false,
  aside = false,
  children,
}: SectionCardProps) {
  const headingId = useId();
  return (
    <section
      aria-labelledby={headingId}
      className={`${empty ? emptyCard : homeCard} flex scroll-mt-32 flex-col gap-3 ${aside ? "p-4" : "p-5"}`}
    >
      <div className="-my-2 flex items-center justify-between gap-2">
        <h2
          id={headingId}
          className="py-2 text-rs-lead leading-rs-snug font-medium text-rs-text"
        >
          {title}
        </h2>
        {action}
      </div>
      {children}
    </section>
  );
}

// A bio longer than about six lines starts collapsed. The text cannot be measured
// before it is drawn, so the line and character counts stand in for its height.
const collapsedLines = 6;
const collapsedChars = 600;

function Bio({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  const textId = useId();
  const long =
    text.split("\n").length > collapsedLines || text.length > collapsedChars;
  return (
    <div>
      <p
        id={textId}
        className={`text-rs-body leading-rs-relaxed whitespace-pre-line text-rs-text ${long && !expanded ? "line-clamp-6" : ""}`}
      >
        {text}
      </p>
      {long && (
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls={textId}
          onClick={() => setExpanded(!expanded)}
          className={`mt-1 ${textButton}`}
        >
          {expanded ? "Show less" : "Show more"}
        </button>
      )}
    </div>
  );
}

const textButton =
  "rounded-rs-2xs text-rs-body font-medium text-rs-accent-strong hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none";

// TagsSection is the "Research interests & skills" card: Interests as soft teal chips and
// Skills as outlined chips, each with its own pencil and dialog on the owner's profile. On
// anyone else's an empty group is left out, and the card when both are empty.
function TagsSection({
  profile,
  edit,
}: {
  profile: Profile;
  edit?: EditControls;
}) {
  const noInterests = profile.interests.length === 0;
  const noSkills = profile.skills.length === 0;
  if (noInterests && noSkills && !edit) return null;
  return (
    <SectionCard
      title="Research interests & skills"
      empty={noInterests && noSkills}
    >
      {(!noInterests || edit) && (
        <TagGroup
          title="Interests"
          section="interests"
          editLabel="Edit research interests"
          edit={edit}
        >
          {noInterests ? (
            <EmptyPrompt
              section="interests"
              label="Add your research interests"
            />
          ) : (
            <Chips
              label="Research interests"
              tags={profile.interests}
              chipClass="bg-rs-accent-soft py-[5px] text-rs-heading"
            />
          )}
        </TagGroup>
      )}
      {(!noSkills || edit) && (
        <TagGroup
          title="Skills"
          section="skills"
          editLabel="Edit skills"
          edit={edit}
        >
          {noSkills ? (
            <EmptyPrompt section="skills" label="Add your skills" />
          ) : (
            <ShowAll total={profile.skills.length} shown={shownSkills}>
              {(limit) => (
                <Chips
                  label="Skills"
                  tags={profile.skills.slice(0, limit)}
                  chipClass="border border-rs-control py-1 text-rs-text"
                />
              )}
            </ShowAll>
          )}
        </TagGroup>
      )}
    </SectionCard>
  );
}

function TagGroup({
  title,
  section,
  editLabel,
  edit,
  children,
}: {
  title: string;
  section: EditSection;
  editLabel: string;
  edit?: EditControls;
  children: ReactNode;
}) {
  const headingId = useId();
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-2.5">
      <div className="-my-2 flex items-center justify-between gap-2">
        <h3
          id={headingId}
          className="py-2 text-rs-caption font-medium text-rs-text-muted"
        >
          {title}
        </h3>
        {edit && (
          <EditButton
            id={editButtonId(section)}
            label={editLabel}
            onClick={() => edit.open(section)}
          />
        )}
      </div>
      {children}
    </section>
  );
}

// ShowAll renders the first `shown` entries of a list and, when there are more, a
// full-width "Show all N" row at the bottom of the card that expands the list in place.
// noun, when given, names the entries ("Show all 4 experiences").
type ShowAllProps = {
  total: number;
  shown: number;
  noun?: string;
  children: (limit: number) => ReactNode;
};

function ShowAll({ total, shown, noun, children }: ShowAllProps) {
  const [expanded, setExpanded] = useState(false);
  const listId = useId();
  const collapsible = total > shown;
  return (
    <>
      <div id={listId}>
        {children(collapsible && !expanded ? shown : total)}
      </div>
      {collapsible && !expanded && (
        <button
          type="button"
          aria-expanded={false}
          aria-controls={listId}
          onClick={() => setExpanded(true)}
          className="-mx-5 -mb-5 flex w-[calc(100%+2.5rem)] items-center justify-center gap-2 rounded-b-card border-t border-rs-divider py-3 text-rs-body font-medium text-rs-text-muted hover:bg-rs-subtle hover:text-rs-text focus-visible:shadow-focus focus-visible:outline-none"
        >
          Show all {total}
          {noun && ` ${noun}`} <span aria-hidden="true">→</span>
        </button>
      )}
    </>
  );
}

function Chips({
  label,
  tags,
  chipClass,
}: {
  label: string;
  tags: Tag[];
  chipClass: string;
}) {
  return (
    <ul aria-label={label} className="flex flex-wrap gap-1.5">
      {tags.map((t) => (
        <li
          key={t.id}
          className={`rounded-rs-pill px-3 text-rs-body leading-rs-snug ${chipClass}`}
        >
          {t.name}
        </li>
      ))}
    </ul>
  );
}

// entryList separates the entries of a section with a divider line, as in the design.
const entryList = "flex flex-col gap-3";
const entryItem =
  "border-t border-rs-divider pt-3 first:border-t-0 first:pt-0";

function EducationList({ entries }: { entries: Education[] }) {
  return (
    <ul aria-label="Education" className={entryList}>
      {entries.map((e) => {
        const study = [e.degree, e.fieldOfStudy].filter(Boolean).join(", ");
        return (
          <li
            key={e.id}
            className={`flex flex-col gap-0.5 text-rs-body ${entryItem}`}
          >
            <p className="font-bold text-rs-text">{e.institution}</p>
            {study && <p className="text-rs-text">{study}</p>}
            <p className="text-rs-caption text-rs-text-muted">
              {yearRange(e.startYear, e.endYear)}
            </p>
            {e.grade && <p className="text-rs-text">Grade: {e.grade}</p>}
            {e.description && <ClampedText text={e.description} />}
          </li>
        );
      })}
    </ul>
  );
}

// ExperienceSection lists work experience in the order the API returns it (FR-4.8). On
// the owner's profile a "+" adds an entry and each entry has a pencil; an empty section
// then prompts for one. On anyone else's an empty section is hidden.
function ExperienceSection({
  entries,
  edit,
}: {
  entries: Experience[];
  edit?: EditControls;
}) {
  if (entries.length === 0 && !edit) return null;
  const action = edit && (
    <AddButton
      id={editButtonId("experience")}
      label="Add experience"
      onClick={() => edit.open("experience")}
    />
  );
  return (
    <SectionCard
      title="Experience"
      action={action}
      empty={entries.length === 0}
    >
      {entries.length === 0 ? (
        <EmptyPrompt section="experience" label="Add experience" />
      ) : (
        <ShowAll
          total={entries.length}
          shown={shownExperience}
          noun="experiences"
        >
          {(limit) => (
            <ul aria-label="Experience" className={entryList}>
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

// ExperienceEntry is one job: the organization's initials on a square tile standing in for
// its logo, the title, "organization · type", "dates · place" and the description.
function ExperienceEntry({
  entry: e,
  edit,
}: {
  entry: Experience;
  edit?: EditControls;
}) {
  const place = [
    e.location,
    e.locationType && locationTypeLabels[e.locationType],
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <li className={`flex items-start gap-3 ${entryItem}`}>
      <span
        aria-hidden="true"
        className="flex size-12 shrink-0 items-center justify-center rounded-control bg-rs-accent-deep text-rs-body font-bold text-rs-surface"
      >
        {initials(e.organization)}
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5 text-rs-body">
        <p className="font-bold text-rs-text">{e.title}</p>
        <p className="text-rs-text">
          {e.organization} · {employmentTypeLabels[e.employmentType]}
        </p>
        <p className="text-rs-caption text-rs-text-muted">
          {experienceDates(e)}
          {place && ` · ${place}`}
        </p>
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

// LanguagesSection lists languages in the order the API returns them, highest proficiency
// first (FR-4.9). On the owner's profile a "+" adds one and each has a pencil; an empty
// section then prompts for one. On anyone else's an empty section is hidden.
function LanguagesSection({
  entries,
  edit,
}: {
  entries: Language[];
  edit?: EditControls;
}) {
  if (entries.length === 0 && !edit) return null;
  const action = edit && (
    <AddButton
      id={editButtonId("languages")}
      label="Add language"
      onClick={() => edit.open("languages")}
    />
  );
  return (
    <SectionCard title="Languages" action={action} empty={entries.length === 0}>
      {entries.length === 0 ? (
        <EmptyPrompt section="languages" label="Add language" />
      ) : (
        <ShowAll total={entries.length} shown={shownLanguages} noun="languages">
          {(limit) => (
            <ul aria-label="Languages" className="flex flex-col gap-2">
              {entries.slice(0, limit).map((l) => (
                <li
                  key={l.id}
                  className="flex items-center gap-3 text-rs-body"
                >
                  <p className="min-w-0 flex-1">
                    <span className="font-bold text-rs-text">{l.name}</span>{" "}
                    <span className="text-rs-text-muted">
                      · {proficiencyLabels[l.proficiency]}
                    </span>
                  </p>
                  {edit && (
                    <EditButton
                      id={languageButtonId(l.id)}
                      label={`Edit ${l.name}`}
                      onClick={() => edit.editLanguage(l)}
                    />
                  )}
                </li>
              ))}
            </ul>
          )}
        </ShowAll>
      )}
    </SectionCard>
  );
}

// A description longer than about three lines starts clamped, as the bio does with six;
// the line and character counts stand in for its height.
const clampedLines = 3;
const clampedChars = 250;

function ClampedText({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  const textId = useId();
  const long =
    text.split("\n").length > clampedLines || text.length > clampedChars;
  return (
    <div className="mt-0.5">
      <p
        id={textId}
        className={`text-rs-body whitespace-pre-line text-rs-text ${long && !expanded ? "line-clamp-3" : ""}`}
      >
        {text}
      </p>
      {long && (
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls={textId}
          onClick={() => setExpanded(!expanded)}
          className={textButton}
        >
          {expanded ? "see less" : "…see more"}
        </button>
      )}
    </div>
  );
}
