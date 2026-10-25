"use client";

import { useState } from "react";

import { Dialog } from "@/components/ui/dialog";

import type { Profile, TagKind } from "./api";
import { TagEditor } from "./tag-input";

type TagsDialogProps = {
  kind: TagKind;
  profile: Profile;
  onClose: () => void;
  // onChanged reloads the profile behind the dialog after each change.
  onChanged: () => void;
  returnFocusId?: string;
};

// TagsDialog edits research interests or skills (FR-4.2, FR-4.3). Changes apply at once,
// so only text typed but not added counts as unsaved.
export function TagsDialog({
  kind,
  profile,
  onClose,
  onChanged,
  returnFocusId,
}: TagsDialogProps) {
  const [hasDraft, setHasDraft] = useState(false);
  return (
    <Dialog
      title={kind === "interests" ? "Edit research interests" : "Edit skills"}
      dirty={hasDraft}
      onClose={onClose}
      returnFocusId={returnFocusId}
    >
      <TagEditor
        kind={kind}
        initial={profile[kind]}
        onChange={onChanged}
        onDraftChange={setHasDraft}
      />
    </Dialog>
  );
}
