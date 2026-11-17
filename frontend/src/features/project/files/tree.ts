import type { ProjectFile } from "./api";

// Paths and the file tree of a project's workspace (FR-12.1). The server checks every path
// (ProjectFilePath in docs/openapi.yaml); these checks only give the message sooner.

export type TreeNode = { file: ProjectFile; name: string; depth: number; children: TreeNode[] };

export const maxDepth = 8;
export const maxPathLength = 255;
const segmentPattern = /^[A-Za-z0-9_.-]{1,100}$/;
const figureExtensions = [".png", ".jpg", ".jpeg", ".pdf"];

export function baseName(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

// parentPath is the folder holding path, "" for the project's top level.
export function parentPath(path: string): string {
  const i = path.lastIndexOf("/");
  return i < 0 ? "" : path.slice(0, i);
}

export function joinPath(folder: string, name: string): string {
  return folder ? `${folder}/${name}` : name;
}

// isInside says whether path is folder itself or anything in it.
export function isInside(path: string, folder: string): boolean {
  const p = path.toLowerCase();
  const f = folder.toLowerCase();
  return p === f || p.startsWith(`${f}/`);
}

export function isFigureName(name: string): boolean {
  const lower = name.toLowerCase();
  return figureExtensions.some((ext) => lower.endsWith(ext));
}

// nameError checks one name (a path segment) and says what is wrong with it, or null.
export function nameError(name: string): string | null {
  if (name === "") return "Enter a name.";
  if (/\s/.test(name)) return "Names can’t contain spaces (LaTeX handles them badly). Use - or _ instead.";
  if (name.startsWith(".")) return "Names can’t start with a dot.";
  if (name.length > 100) return "Names can be at most 100 characters.";
  if (!segmentPattern.test(name)) return "Use only letters A to Z, digits, -, _ and dots.";
  return null;
}

// pathError checks a whole new path made of folder and name, for a text file, a folder or a
// figure, and says what is wrong with it, or null.
export function pathError(folder: string, name: string, kind: ProjectFile["kind"]): string | null {
  const err = nameError(name);
  if (err) return err;
  const path = joinPath(folder, name);
  if (path.split("/").length > maxDepth) return `Files can be at most ${maxDepth} folders deep.`;
  if (path.length > maxPathLength) return `The whole path can be at most ${maxPathLength} characters.`;
  if (kind === "text" && isFigureName(name)) return "Figures (.png, .jpg, .pdf) are uploaded, not created. Use Upload.";
  return null;
}

// figureTypes maps a figure's content type to the extension its name gets when it has none
// of its own (a file the browser knows by type only).
const figureTypes: Record<string, string> = { "image/png": ".png", "image/jpeg": ".jpg", "application/pdf": ".pdf" };
const figureExtension = /\.(png|jpe?g|pdf)$/;

// figureName turns an uploaded file's name into one the path rules accept, so a name alone
// never stops an upload (FR-12.2): lower case; every run of spaces and other characters
// becomes one "-"; no "-" or "." at the start or around the extension; at most maxLength
// characters with the extension; "figure" when nothing usable is left. A name without a
// figure extension gets the one of its type. "WhatsApp Image 2026-11-17 at 9.10.25 PM.jpeg"
// becomes "whatsapp-image-2026-11-17-at-9.10.25-pm.jpeg".
export function figureName(name: string, type = "", maxLength = 100): string {
  let lower = name.normalize("NFKD").toLowerCase();
  if (!figureExtension.test(lower.trim()) && figureTypes[type]) lower = `${lower}${figureTypes[type]}`;
  const dot = lower.lastIndexOf(".");
  const stem = dot > 0 ? lower.slice(0, dot) : lower;
  const ext = dot > 0 ? lower.slice(dot).replace(/[^a-z0-9.]/g, "") : "";
  const clean = (s: string) =>
    s
      .replace(/[^a-z0-9_.-]+/g, "-")
      .replace(/-{2,}/g, "-")
      .replace(/\.{2,}/g, ".")
      .replace(/^[.-]+/, "")
      .replace(/[.-]+$/, "");
  const base = clean(stem.slice(0, 200)).slice(0, Math.max(1, maxLength - ext.length)).replace(/[.-]+$/, "");
  return (base || "figure") + ext;
}

// uniqueName gives name a "-2", "-3", ... before its extension until no name in taken (lower
// case) has it, ignoring letter case as the server does.
export function uniqueName(name: string, taken: ReadonlySet<string>): string {
  if (!taken.has(name.toLowerCase())) return name;
  const dot = name.lastIndexOf(".");
  const stem = dot > 0 ? name.slice(0, dot) : name;
  const ext = dot > 0 ? name.slice(dot) : "";
  for (let n = 2; ; n++) {
    const candidate = `${stem.slice(0, 100 - ext.length - String(n).length - 1)}-${n}${ext}`;
    if (!taken.has(candidate.toLowerCase())) return candidate;
  }
}

const byName = (a: TreeNode, b: TreeNode) => {
  if ((a.file.kind === "folder") !== (b.file.kind === "folder")) return a.file.kind === "folder" ? -1 : 1;
  return a.name.toLowerCase().localeCompare(b.name.toLowerCase());
};

// buildTree turns the flat list into nested nodes: folders first, then files, each by name
// ignoring case. An entry whose folder is missing from the list goes at the top level.
export function buildTree(items: ProjectFile[]): TreeNode[] {
  const folders = new Map<string, TreeNode>();
  const nodes = items.map((file) => {
    const node: TreeNode = { file, name: baseName(file.path), depth: 0, children: [] };
    if (file.kind === "folder") folders.set(file.path.toLowerCase(), node);
    return node;
  });
  const roots: TreeNode[] = [];
  for (const node of nodes) {
    const parent = folders.get(parentPath(node.file.path).toLowerCase());
    (parent ? parent.children : roots).push(node);
  }
  const settle = (list: TreeNode[], depth: number) => {
    list.sort(byName);
    for (const node of list) {
      node.depth = depth;
      settle(node.children, depth + 1);
    }
  };
  settle(roots, 0);
  return roots;
}

// visibleNodes lists the nodes a reader sees, in order: the children of expanded folders only.
export function visibleNodes(tree: TreeNode[], expanded: ReadonlySet<string>): TreeNode[] {
  const out: TreeNode[] = [];
  const walk = (list: TreeNode[]) => {
    for (const node of list) {
      out.push(node);
      if (node.file.kind === "folder" && expanded.has(node.file.id)) walk(node.children);
    }
  };
  walk(tree);
  return out;
}

// countInside is how many entries a folder holds, at every level.
export function countInside(items: ProjectFile[], folder: ProjectFile): number {
  return items.filter((f) => f.id !== folder.id && isInside(f.path, folder.path)).length;
}

// folderAncestors lists the IDs of the folders above path, to expand them so it shows.
export function folderAncestors(items: ProjectFile[], path: string): string[] {
  return items.filter((f) => f.kind === "folder" && isInside(path, f.path) && f.path !== path).map((f) => f.id);
}
