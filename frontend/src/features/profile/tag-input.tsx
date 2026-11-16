"use client";

import { useState } from "react";

import { addTag, removeTag, type Tag, type TagKind } from "./api";
import { ChipInput } from "./chip-input";
import { genericError, MAX_INTERESTS, MAX_SKILLS, sameTag } from "./validation";

export { suggestDelay } from "./chip-input";

// The chips look as they do on the profile page (profile-sections.tsx).
const words: Record<
  TagKind,
  { label: string; one: string; limit: number; chip: string }
> = {
  interests: {
    label: "Research interests",
    one: "research interest",
    limit: MAX_INTERESTS,
    chip: "bg-rs-accent-soft text-rs-heading",
  },
  skills: {
    label: "Skills",
    one: "skill",
    limit: MAX_SKILLS,
    chip: "border border-rs-control text-rs-text",
  },
};

const byName = (a: Tag, b: Tag) =>
  a.name.localeCompare(b.name, undefined, { sensitivity: "base" });

type TagEditorProps = {
  kind: TagKind;
  initial: Tag[];
  // onChange is called after each tag the server added or removed.
  onChange: () => void;
  // onDraftChange says whether the input holds text that has not been added yet.
  onDraftChange?: (hasDraft: boolean) => void;
};

// TagEditor edits the user's research interests or skills (FR-4.2): the chips, each with
// a remove button, and a combobox that suggests existing tags while the user types
// (FR-4.3). Every change is sent at once; there is nothing to save.
export function TagEditor({ kind, initial, onChange, onDraftChange }: TagEditorProps) {
  const { label, one, limit, chip } = words[kind];
  const [tags, setTags] = useState(initial);

  async function add(name: string): Promise<string | null> {
    const res = await addTag(kind, name);
    if (!res.ok) {
      return res.code === "tag_limit_reached" || res.code === "invalid_tag_name" ? res.message : genericError;
    }
    const tag = res.data;
    setTags((ts) => (ts.some((t) => t.id === tag.id) ? ts : [...ts, tag].sort(byName)));
    onChange();
    return null;
  }

  async function remove(name: string): Promise<string | null> {
    const tag = tags.find((t) => sameTag(t.name, name));
    if (!tag) return null;
    const res = await removeTag(kind, tag.id);
    // not_found means it is already gone, which is what the user asked for.
    if (!res.ok && res.code !== "not_found") return genericError;
    setTags((ts) => ts.filter((t) => t.id !== tag.id));
    onChange();
    return null;
  }

  return (
    <ChipInput
      kind={kind}
      label={label}
      inputLabel={`Add a ${one}`}
      limit={limit}
      chip={chip}
      names={tags.map((t) => t.name)}
      onAdd={add}
      onRemove={remove}
      onDraftChange={onDraftChange}
    />
  );
}
