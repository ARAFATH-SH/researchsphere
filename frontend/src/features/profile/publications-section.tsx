"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import type { Profile } from "./api";
import { AddButton, editButtonId } from "./edit-button";
import { PublicationItem } from "./publication-item";
import { PublicationMenu } from "./publication-menu";
import { EmptyPrompt, SectionCard } from "./profile-sections";
import {
  listMyPublications,
  listPublications,
  type Publication,
  type PublicationPage,
} from "./publications-api";

// shownPublications is how many publications the profile shows before "Show all".
export const shownPublications = 3;

// PublicationControls are given only on the owner's profile.
export type PublicationControls = {
  add: () => void;
  edit: (publication: Publication) => void;
  removed: () => void;
};

type Loaded =
  | { status: "ok"; page: PublicationPage }
  | { status: "error" };

// PublicationsSection is the Publications card (FR-5.5), between "Research interests &
// skills" and Experience as in design/screens/Profile.html. It shows the first three
// confirmed publications and, when there are more, "Show all N publications", which goes to
// /profile/<userId>/publications. The list is fetched when the profile's count or
// refreshKey changes, so an add, import, edit or remove shows without a page reload. On the
// owner's profile a "+" opens the add dialog, each item has a menu, and an empty section
// prompts to add one; on anyone else's an empty section is hidden.
export function PublicationsSection({
  profile,
  refreshKey = 0,
  controls,
}: {
  profile: Profile;
  refreshKey?: number;
  controls?: PublicationControls;
}) {
  const own = controls !== undefined;
  const count = profile.publicationCount;
  const [attempt, setAttempt] = useState(0);
  const owner = own ? "me" : profile.userId;
  const key = `${owner}:${count}:${refreshKey}:${attempt}`;
  const [result, setResult] = useState<{ key: string; value: Loaded } | null>(
    null,
  );

  useEffect(() => {
    if (count === 0) return;
    let current = true;
    void (
      own
        ? listMyPublications(1, shownPublications)
        : listPublications(profile.userId, 1, shownPublications)
    ).then((res) => {
      if (!current) return;
      setResult({
        key,
        value: res.ok ? { status: "ok", page: res.data } : { status: "error" },
      });
    });
    return () => {
      current = false;
    };
  }, [own, profile.userId, count, key]);

  if (count === 0 && !own) return null;
  const action = controls && (
    <AddButton
      id={editButtonId("publications")}
      label="Add publication"
      onClick={controls.add}
    />
  );
  if (count === 0)
    return (
      <SectionCard title="Publications" action={action} empty>
        <EmptyPrompt section="publications" label="Add publication" />
      </SectionCard>
    );

  // While a new list of the same user loads, the last one stays on screen.
  const value =
    result && result.key.startsWith(`${owner}:`) ? result.value : undefined;
  let body;
  if (!value) {
    body = <p className="text-rs-body text-rs-text-muted">Loading publications…</p>;
  } else if (value.status === "error") {
    body = (
      <p className="text-rs-body text-rs-text-muted">
        We couldn&apos;t load the publications.{" "}
        <button
          type="button"
          onClick={() => setAttempt((n) => n + 1)}
          className="rounded-rs-2xs font-medium text-rs-accent-strong hover:underline focus-visible:shadow-focus focus-visible:outline-none"
        >
          Try again
        </button>
      </p>
    );
  } else {
    const { items, total } = value.page;
    body = (
      <>
        <ul aria-label="Publications" className="flex flex-col gap-3">
          {items.map((p) => (
            <PublicationItem
              key={p.id}
              publication={p}
              actions={
                controls && (
                  <PublicationMenu
                    publication={p}
                    onEdit={() => controls.edit(p)}
                    onRemoved={controls.removed}
                  />
                )
              }
            />
          ))}
        </ul>
        {total > shownPublications && (
          <Link
            href={`/profile/${profile.userId}/publications`}
            className="-mx-5 -mb-5 flex items-center justify-center gap-2 rounded-b-card border-t border-rs-divider py-3 text-rs-body font-medium text-rs-text-muted hover:bg-rs-subtle hover:text-rs-text focus-visible:shadow-focus focus-visible:outline-none"
          >
            Show all {total} publications <span aria-hidden="true">→</span>
          </Link>
        )}
      </>
    );
  }
  return (
    <SectionCard title="Publications" action={action}>
      {body}
    </SectionCard>
  );
}
