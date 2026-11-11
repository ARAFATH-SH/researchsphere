"use client";

import type { MouseEvent, ReactNode } from "react";

import { Spinner } from "@/components/ui/spinner";

import { type Attachment, ensureSession } from "./api";
import { attachmentUrl, fileKind, fileSize } from "./attachments";
import type { PendingFile } from "./use-attachments";

// navigation.go opens a URL; a test replaces it, as jsdom cannot navigate.
export const navigation = { go: (url: string) => window.location.assign(url) };

function FileBadge({ contentType, name }: { contentType: string; name: string }) {
  const kind = fileKind(contentType, name);
  return (
    <span
      aria-hidden="true"
      className={`flex h-11 w-10 flex-none items-center justify-center rounded-rs-xs text-[11px] font-bold ${kind.tone}`}
    >
      {kind.label}
    </span>
  );
}

// AttachmentCard is one file of a message (FR-16.7), as design/screens/Messaging.html draws it:
// a type badge, the name and size, and what the malware scan allows. Files are never shown
// inline: a clean one downloads. Every state is said in words, not by colour.
export function AttachmentCard({ attachment }: { attachment: Attachment }) {
  const { id, name, size, contentType, status } = attachment;
  const href = attachmentUrl(id);

  // A plain link to the API carries the access token, which may have expired: a normal click
  // renews the session first. A middle click or "open in new tab" stays a plain link.
  async function download(event: MouseEvent<HTMLAnchorElement>) {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    if (await ensureSession()) navigation.go(href);
  }

  return (
    <div className="flex max-w-full min-w-0 items-center gap-2.5 rounded-card border border-rs-border bg-rs-surface px-3 py-2.5 text-rs-text">
      <FileBadge contentType={contentType} name={name} />
      <span className="flex min-w-0 flex-col">
        <strong className="truncate text-rs-body" title={name}>
          {name}
        </strong>
        {status === "pending" && (
          <span className="flex items-center gap-1.5 text-rs-caption text-rs-text-muted">
            <span>{fileSize(size)} ·</span>
            <Spinner />
            <span>Scanning for viruses…</span>
          </span>
        )}
        {status === "clean" && (
          <span className="text-rs-caption text-rs-text-muted">
            {fileSize(size)} ·{" "}
            <a
              href={href}
              onClick={(e) => void download(e)}
              aria-label={`Download ${name}`}
              className="rounded-rs-2xs font-medium text-rs-accent-strong underline-offset-2 hover:underline focus-visible:shadow-focus focus-visible:outline-none"
            >
              Download
            </a>
          </span>
        )}
        {status === "infected" && (
          <span className="text-rs-caption font-medium text-rs-danger">This file was removed because it failed a security check.</span>
        )}
        {status === "failed" && (
          <span className="text-rs-caption text-rs-text-muted">This file couldn&apos;t be checked and isn&apos;t available.</span>
        )}
      </span>
    </div>
  );
}

// AttachmentChip is a file in the composer: its upload's progress, then its scan, with a way
// to take it out.
export function AttachmentChip({ file, onRemove }: { file: PendingFile; onRemove: () => void }) {
  const scan = file.upload?.status;
  let line: ReactNode = fileSize(file.size);
  let tone = "text-rs-text-muted";
  if (file.state === "error") {
    line = file.error;
    tone = "text-rs-danger";
  } else if (scan === "infected") {
    line = "This file failed a security check. Remove it to send.";
    tone = "text-rs-danger";
  } else if (scan === "failed") {
    line = "This file couldn't be checked. Remove it to send.";
    tone = "text-rs-danger";
  } else if (file.state === "uploading") {
    line = `Uploading… ${Math.round(file.progress * 100)}%`;
  }
  return (
    <li className="flex w-full max-w-xs min-w-0 items-center gap-2 rounded-card border border-rs-border bg-rs-surface px-2.5 py-2">
      <FileBadge contentType={file.upload?.contentType ?? file.contentType} name={file.name} />
      <span className="flex min-w-0 flex-1 flex-col gap-1">
        <strong className="truncate text-rs-caption text-rs-text" title={file.name}>
          {file.name}
        </strong>
        {file.state === "uploading" && (
          <progress
            value={file.progress}
            max={1}
            aria-label={`Uploading ${file.name}`}
            className="h-1.5 w-full overflow-hidden rounded-rs-pill [&::-moz-progress-bar]:bg-rs-accent-strong [&::-webkit-progress-bar]:bg-rs-subtle [&::-webkit-progress-value]:bg-rs-accent-strong"
          />
        )}
        <span className={`text-rs-tiny ${tone}`}>{line}</span>
      </span>
      <button
        type="button"
        onClick={onRemove}
        aria-label={`Remove ${file.name}`}
        className="flex-none rounded-full p-1 text-rs-text-muted hover:bg-rs-subtle hover:text-rs-text focus-visible:shadow-focus focus-visible:outline-none"
      >
        <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" className="size-4">
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      </button>
    </li>
  );
}
