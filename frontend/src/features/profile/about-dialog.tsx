"use client";

import { type FormEvent, useState } from "react";

import { Dialog } from "@/components/ui/dialog";
import { FormError, TextAreaField } from "@/components/ui/field";

import { type Profile, updateMyProfile } from "./api";
import { orNull, profileUpdate } from "./profile-update";
import { FormActions } from "./save-button";
import {
  fieldForProfileCode,
  genericError,
  length,
  MAX_BIO_LENGTH,
  validateBio,
} from "./validation";

type DialogProps = {
  profile: Profile;
  onClose: () => void;
  onSaved: () => void;
  returnFocusId?: string;
};

// AboutDialog edits the bio, with a live count of the characters left (FR-4.1).
export function AboutDialog({
  profile,
  onClose,
  onSaved,
  returnFocusId,
}: DialogProps) {
  const initial = profile.bio ?? "";
  const [bio, setBio] = useState(initial);
  const [error, setError] = useState<string>();
  const [formError, setFormError] = useState<string>();
  const [saving, setSaving] = useState(false);
  const count = length(bio.trim());

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(undefined);
    const found = validateBio(bio);
    setError(found);
    if (found) return;

    setSaving(true);
    const res = await updateMyProfile(
      profileUpdate(profile, { bio: orNull(bio) }),
    );
    setSaving(false);
    if (res.ok) return onSaved();
    if (fieldForProfileCode(res.code) === "bio") setError(res.message);
    else setFormError(genericError);
  }

  return (
    <Dialog
      title="Edit about"
      dirty={bio !== initial}
      onClose={onClose}
      returnFocusId={returnFocusId}
    >
      <form noValidate onSubmit={onSubmit} className="space-y-3">
        {formError ? <FormError look="rs">{formError}</FormError> : null}
        <TextAreaField
          id="about-bio"
          name="bio"
          label="Bio"
          look="rs"
          rows={8}
          error={error}
          value={bio}
          onChange={(e) => setBio(e.target.value)}
          counter={{ count, max: MAX_BIO_LENGTH }}
        />
        <FormActions saving={saving} />
      </form>
    </Dialog>
  );
}
