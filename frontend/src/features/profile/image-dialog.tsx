"use client";

import { type ChangeEvent, useEffect, useRef, useState } from "react";

import { buttonClass } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";
import { initials } from "@/lib/initials";

import { type ImageKind, removeProfileImage, uploadProfileImage } from "./api";
import { checkImageFile, imageMessages, imageTypes, uploadErrorMessage } from "./image-upload";

type ImageDialogProps = {
  kind: ImageKind;
  // name is the user's full name, for the alt text and the initials.
  name: string;
  // currentUrl is the stored image, or null when none is set.
  currentUrl: string | null;
  onClose: () => void;
  // onChanged runs after an upload or delete succeeded, with the toast to show.
  onChanged: (message: string) => void;
  returnFocusId?: string;
};

const words: Record<ImageKind, { title: string; upload: string; what: string; done: string; deleted: string; instead: string }> = {
  photo: {
    title: "Profile photo",
    upload: "Upload photo",
    what: "profile photo",
    done: "Photo updated",
    deleted: "Photo deleted",
    instead: "People will see your initials instead.",
  },
  cover: {
    title: "Cover image",
    upload: "Upload image",
    what: "cover image",
    done: "Cover image updated",
    deleted: "Cover image deleted",
    instead: "Your profile will show the default banner instead.",
  },
};

type Busy = "upload" | "delete" | null;

// ImageDialog shows the profile photo or cover image and lets the owner replace or delete
// it (FR-4.6). A chosen file is checked here first (type and size) and previewed cropped
// the way the server crops it, a centred square or 4:1; the server checks it again.
export function ImageDialog({ kind, name, currentUrl, onClose, onChanged, returnFocusId }: ImageDialogProps) {
  const w = words[kind];
  const input = useRef<HTMLInputElement>(null);
  const saveRef = useRef<HTMLButtonElement>(null);
  const keepRef = useRef<HTMLButtonElement>(null);
  const deleteRef = useRef<HTMLButtonElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<string | null>(null);
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState<Busy>(null);
  const [confirming, setConfirming] = useState(false);
  const uploadRef = useRef<HTMLButtonElement>(null);
  // focusNext names the button that gets the focus after the next render, since the
  // button that had it is gone.
  const focusNext = useRef<"upload" | "save" | "keep" | "delete" | null>(null);

  // The local preview URL is released when it is replaced or the dialog goes away.
  const previewRef = useRef<string | null>(null);
  useEffect(() => () => release(previewRef.current), []);

  useEffect(() => {
    if (!focusNext.current) return;
    const target = { upload: uploadRef, save: saveRef, keep: keepRef, delete: deleteRef }[focusNext.current];
    focusNext.current = null;
    target.current?.focus();
  });

  function showPreview(next: File | null) {
    release(previewRef.current);
    previewRef.current = next ? URL.createObjectURL(next) : null;
    setFile(next);
    setPreview(previewRef.current);
  }

  function onChoose(event: ChangeEvent<HTMLInputElement>) {
    const chosen = event.target.files?.[0];
    // Let the same file be chosen again after a refusal.
    event.target.value = "";
    if (!chosen) return;
    const refusal = checkImageFile(kind, chosen);
    setError(refusal);
    if (refusal) return;
    showPreview(chosen);
    focusNext.current = "save";
  }

  async function save() {
    if (!file) return;
    setError(undefined);
    setBusy("upload");
    const res = await uploadProfileImage(kind, file);
    setBusy(null);
    if (res.ok) onChanged(w.done);
    else setError(uploadErrorMessage(kind, res));
  }

  async function remove() {
    setError(undefined);
    setBusy("delete");
    const res = await removeProfileImage(kind);
    setBusy(null);
    if (res.ok) {
      onChanged(w.deleted);
      return;
    }
    setConfirming(false);
    setError(imageMessages.remove);
    focusNext.current = "delete";
  }

  const disabled = busy !== null;
  const outline = buttonClass({ variant: "outline", size: "md" });
  const primary = buttonClass({ variant: "primary", size: "md" });

  return (
    <Dialog
      title={w.title}
      dirty={file !== null}
      // While a request runs the dialog stays open, so its answer is not lost.
      onClose={() => {
        if (!disabled) onClose();
      }}
      returnFocusId={returnFocusId}
    >
      <div className="space-y-4">
        {error && <FormError>{error}</FormError>}
        <Frame kind={kind} name={name} src={preview ?? currentUrl} local={preview !== null} />
        {confirming ? (
          <div role="group" aria-labelledby="image-delete-question" className="space-y-3">
            <p id="image-delete-question" className="font-semibold text-text">
              Delete your {w.what}?
            </p>
            <p className="text-sm text-text-muted">{w.instead}</p>
            <div className="flex flex-wrap justify-end gap-2">
              <button
                ref={keepRef}
                type="button"
                disabled={disabled}
                onClick={() => {
                  setConfirming(false);
                  focusNext.current = "delete";
                }}
                className={outline}
              >
                Keep
              </button>
              <button
                type="button"
                disabled={disabled}
                onClick={remove}
                className={buttonClass({ variant: "danger", size: "md" })}
              >
                {busy === "delete" && <Spinner />}
                {busy === "delete" ? "Deleting…" : "Delete"}
              </button>
            </div>
          </div>
        ) : file ? (
          <div className="flex flex-wrap justify-end gap-2">
            <button
              type="button"
              disabled={disabled}
              onClick={() => {
                setError(undefined);
                showPreview(null);
                focusNext.current = "upload";
              }}
              className={outline}
            >
              Cancel
            </button>
            <button ref={saveRef} type="button" disabled={disabled} onClick={save} className={primary}>
              {busy === "upload" && <Spinner />}
              {busy === "upload" ? "Uploading…" : "Save"}
            </button>
          </div>
        ) : (
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="text-sm text-text-muted">
              JPG, PNG or WebP, up to {kind === "photo" ? "5" : "8"} MB.
            </p>
            <div className="flex flex-wrap justify-end gap-2">
              <button
                ref={uploadRef}
                type="button"
                disabled={disabled}
                onClick={() => input.current?.click()}
                className={primary}
              >
                {w.upload}
              </button>
              {currentUrl && (
                <button
                  ref={deleteRef}
                  type="button"
                  disabled={disabled}
                  onClick={() => {
                    setError(undefined);
                    setConfirming(true);
                    focusNext.current = "keep";
                  }}
                  className={buttonClass({ variant: "text", size: "md" })}
                >
                  Delete
                </button>
              )}
              <button type="button" disabled={disabled} onClick={onClose} className={outline}>
                Cancel
              </button>
            </div>
          </div>
        )}
        {/* Last in the dialog, so the Dialog's first-focus rule never lands on it. */}
        <input
          ref={input}
          type="file"
          hidden
          aria-label={`Choose a ${w.what}`}
          accept={imageTypes.join(",")}
          onChange={onChoose}
        />
      </div>
    </Dialog>
  );
}

function release(url: string | null) {
  if (url) URL.revokeObjectURL(url);
}

// Frame shows the image cropped as the server stores it: a centred circle-cut square for
// the photo, a centred 4:1 band for the cover. object-cover crops from the centre, as the
// server does.
function Frame({ kind, name, src, local }: { kind: ImageKind; name: string; src: string | null; local: boolean }) {
  const box =
    kind === "photo"
      ? "mx-auto flex size-40 items-center justify-center overflow-hidden rounded-full bg-primary text-5xl font-semibold text-surface"
      : "aspect-[4/1] w-full overflow-hidden rounded-lg bg-linear-to-r from-primary to-secondary";
  const alt = local ? `Preview of the new ${words[kind].what}` : kind === "photo" ? name : `Cover image of ${name}`;
  return (
    <div className={box}>
      {src ? (
        // A local blob: URL or the media redirect; next/image can optimise neither.
        // eslint-disable-next-line @next/next/no-img-element
        <img src={src} alt={alt} className="size-full object-cover" />
      ) : kind === "photo" ? (
        <span role="img" aria-label={name}>
          {initials(name)}
        </span>
      ) : (
        <span role="img" aria-label="No cover image" className="block size-full" />
      )}
    </div>
  );
}
