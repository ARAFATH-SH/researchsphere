import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { useRealtimeEvent } from "@/features/realtime/use-realtime";

import { type Attachment, type AttachmentStatus, type AttachmentUpload, uploadAttachment } from "./api";
import { checkFile, maxFiles, uploadError } from "./attachments";

// PendingFile is a file in the composer: uploading, uploaded (with the server's record, whose
// status is the malware scan), or refused.
export type PendingFile = {
  key: string;
  name: string;
  size: number;
  contentType: string;
  progress: number;
  state: "uploading" | "uploaded" | "error";
  error: string | null;
  upload: AttachmentUpload | null;
};

export type Attachments = {
  files: PendingFile[];
  // notice says why files were not attached: the client's own checks.
  notice: string | null;
  add: (files: File[]) => void;
  // remove takes a file out, cancelling its upload; clear empties the composer after a send.
  remove: (key: string) => void;
  clear: () => void;
  uploading: boolean;
  // blocked is true while a file that cannot be sent is listed: a refused upload, or one whose
  // scan found something or failed. The server would refuse the whole message.
  blocked: boolean;
  ready: Attachment[];
};

let lastKey = 0;

function isBlocked(f: PendingFile): boolean {
  return f.state === "error" || f.upload?.status === "infected" || f.upload?.status === "failed";
}

// useAttachments holds the files of the next message (FR-16.7). Each uploads as soon as it is
// added, with progress. The scan's result arrives on the socket with a null messageId, only to
// the uploader's tabs; one that comes before the upload's answer waits for it.
export function useAttachments(conversationId: string): Attachments {
  const [files, setFiles] = useState<PendingFile[]>([]);
  const [notice, setNotice] = useState<string | null>(null);
  const count = useRef(0);
  const controllers = useRef(new Map<string, AbortController>());
  const early = useRef(new Map<string, AttachmentStatus>());

  useEffect(() => {
    count.current = files.length;
  });

  useEffect(() => {
    const running = controllers.current;
    return () => running.forEach((c) => c.abort());
  }, []);

  const change = useCallback((key: string, patch: Partial<PendingFile>) => {
    setFiles((prev) => prev.map((f) => (f.key === key ? { ...f, ...patch } : f)));
  }, []);

  const start = useCallback(
    (key: string, file: File) => {
      const controller = new AbortController();
      controllers.current.set(key, controller);
      void uploadAttachment(conversationId, file, {
        signal: controller.signal,
        onProgress: (progress) => change(key, { progress }),
      }).then((res) => {
        controllers.current.delete(key);
        if (!res.ok) {
          if (res.code !== "aborted") change(key, { state: "error", error: uploadError(res) });
          return;
        }
        const status = early.current.get(res.data.id) ?? res.data.status;
        early.current.delete(res.data.id);
        change(key, { state: "uploaded", progress: 1, upload: { ...res.data, status } });
      });
    },
    [conversationId, change],
  );

  const add = useCallback(
    (picked: File[]) => {
      const problems: string[] = [];
      const accepted: { pending: PendingFile; file: File }[] = [];
      let room = maxFiles - count.current;
      for (const file of picked) {
        const problem = checkFile(file);
        if (problem) {
          problems.push(problem);
          continue;
        }
        if (room <= 0) {
          problems.push(`You can attach up to ${maxFiles} files to a message.`);
          break;
        }
        room--;
        accepted.push({
          file,
          pending: {
            key: `file-${++lastKey}`,
            name: file.name,
            size: file.size,
            contentType: file.type,
            progress: 0,
            state: "uploading",
            error: null,
            upload: null,
          },
        });
      }
      setNotice(problems.length > 0 ? problems.join(" ") : null);
      if (accepted.length === 0) return;
      count.current += accepted.length;
      setFiles((prev) => [...prev, ...accepted.map((a) => a.pending)]);
      accepted.forEach(({ pending, file }) => start(pending.key, file));
    },
    [start],
  );

  const remove = useCallback((key: string) => {
    controllers.current.get(key)?.abort();
    controllers.current.delete(key);
    count.current = Math.max(0, count.current - 1);
    setFiles((prev) => prev.filter((f) => f.key !== key));
    setNotice(null);
  }, []);

  const clear = useCallback(() => {
    count.current = 0;
    setFiles([]);
    setNotice(null);
  }, []);

  useRealtimeEvent((event) => {
    if (event.type !== "attachment.updated" || event.data.messageId !== null || event.data.conversationId !== conversationId) return;
    const { attachmentId, status } = event.data;
    setFiles((prev) => {
      if (!prev.some((f) => f.upload?.id === attachmentId)) {
        early.current.set(attachmentId, status);
        return prev;
      }
      return prev.map((f) => (f.upload?.id === attachmentId ? { ...f, upload: { ...f.upload, status } } : f));
    });
  });

  return useMemo(() => {
    const ready = files
      .filter((f) => f.state === "uploaded" && f.upload && !isBlocked(f))
      .map(({ upload }) => {
        const { id, name, size, contentType, status } = upload as AttachmentUpload;
        return { id, name, size, contentType, status };
      });
    return {
      files,
      notice,
      add,
      remove,
      clear,
      uploading: files.some((f) => f.state === "uploading"),
      blocked: files.some(isBlocked),
      ready,
    };
  }, [files, notice, add, remove, clear]);
}
