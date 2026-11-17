import type { ProjectFile, ProjectFileTree } from "./api";

// Test data for the workspace.

const base = {
  size: 0,
  version: 1,
  contentType: null,
  scanStatus: null,
  updatedAt: "2026-11-17T10:00:00Z",
  updatedBy: null,
} as const;

export function textFile(id: string, path: string, extra: Partial<ProjectFile> = {}): ProjectFile {
  return { ...base, id, path, kind: "text", ...extra };
}

export function folder(id: string, path: string): ProjectFile {
  return { ...base, id, path, kind: "folder" };
}

export function figure(id: string, path: string, extra: Partial<ProjectFile> = {}): ProjectFile {
  const contentType = path.endsWith(".pdf") ? "application/pdf" : path.endsWith(".png") ? "image/png" : "image/jpeg";
  return { ...base, id, path, kind: "binary", contentType, scanStatus: "clean", size: 1000, ...extra };
}

// storage is a tree's storage: nothing used of the default limits unless a test says.
export const storage = (usedBytes = 0, limitBytes = 500 << 20, figureMaxBytes = 50 << 20) => ({
  usedBytes,
  limitBytes,
  figureMaxBytes,
});

export function tree(items: ProjectFile[], mainFilePath = "main.tex", used = storage()): ProjectFileTree {
  return { mainFilePath, items, storage: used };
}
