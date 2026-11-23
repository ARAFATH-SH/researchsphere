import { projectID } from "../../test-projects";
import type { Compile } from "./api";

// compile builds a Compile for tests; queued by default.
export function compile(id: string, patch: Partial<Compile> = {}): Compile {
  return {
    id,
    projectId: projectID,
    status: "queued",
    engine: "pdflatex",
    mainFilePath: "main.tex",
    requestedBy: "u-me",
    createdAt: "2026-11-23T10:00:00Z",
    startedAt: null,
    finishedAt: null,
    durationMs: null,
    failure: null,
    errors: [],
    skipped: [],
    liveTextConfirmed: true,
    pages: null,
    hasLog: false,
    hasPdf: false,
    ...patch,
  };
}

export function succeeded(id: string, patch: Partial<Compile> = {}): Compile {
  return compile(id, {
    status: "succeeded",
    startedAt: "2026-11-23T10:00:01Z",
    finishedAt: "2026-11-23T10:00:09Z",
    durationMs: 7800,
    pages: 6,
    hasLog: true,
    hasPdf: true,
    ...patch,
  });
}
