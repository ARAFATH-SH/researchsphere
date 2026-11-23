"use client";

import { type KeyboardEvent, useId } from "react";

import type { ProjectFile } from "../files/api";
import { buildTree, type TreeNode, visibleNodes } from "../files/tree";
import type { PresenceFiles, PresenceUser } from "./collab/types";

// FileTree shows a project's files and folders (FR-12.1) as an ARIA tree: one tab stop, the
// arrow keys move and open folders, Home and End jump, Enter opens a file or toggles a folder.
// For an editor or the owner F2 renames, Delete deletes and M moves the focused entry; the same
// actions show as buttons on the selected row, for the mouse.

export const treeItemId = (fileId: string) => `tree-item-${fileId}`;

type Props = {
  items: ProjectFile[];
  expanded: ReadonlySet<string>;
  selectedId: string | null;
  openId: string | null;
  canEdit: boolean;
  // presence says who edits which file now (FR-12.5); null while unknown.
  presence?: PresenceFiles | null;
  // mainFilePath is the file a compile starts from (FR-13.1), marked "Main".
  mainFilePath?: string;
  onSelect: (file: ProjectFile) => void;
  onToggle: (file: ProjectFile, open?: boolean) => void;
  onOpen: (file: ProjectFile) => void;
  onRename: (file: ProjectFile) => void;
  onMove: (file: ProjectFile) => void;
  onDelete: (file: ProjectFile) => void;
};

export function FileTree(props: Props) {
  const { items, expanded, selectedId, canEdit, onSelect, onToggle, onOpen, onRename, onMove, onDelete } = props;
  const hintId = useId();
  const tree = buildTree(items);
  const visible = visibleNodes(tree, expanded);
  // The tab stop is the selected entry, or the first one.
  const focusId = visible.some((n) => n.file.id === selectedId) ? selectedId : (visible[0]?.file.id ?? null);

  function focus(node: TreeNode | undefined) {
    if (!node) return;
    onSelect(node.file);
    document.getElementById(treeItemId(node.file.id))?.focus();
  }

  function activate(node: TreeNode) {
    if (node.file.kind === "folder") onToggle(node.file);
    else onOpen(node.file);
  }

  function onKeyDown(event: KeyboardEvent) {
    const i = visible.findIndex((n) => n.file.id === focusId);
    const node = visible[i];
    if (!node) return;
    const isFolder = node.file.kind === "folder";
    const isOpen = isFolder && expanded.has(node.file.id);
    let handled = true;
    switch (event.key) {
      case "ArrowDown":
        focus(visible[i + 1]);
        break;
      case "ArrowUp":
        focus(visible[i - 1]);
        break;
      case "Home":
        focus(visible[0]);
        break;
      case "End":
        focus(visible.at(-1));
        break;
      case "ArrowRight":
        if (isFolder && !isOpen) onToggle(node.file, true);
        else if (isOpen) focus(node.children[0] && visible[i + 1]);
        break;
      case "ArrowLeft":
        if (isOpen) onToggle(node.file, false);
        else focus(visible.slice(0, i).findLast((n) => n.depth === node.depth - 1));
        break;
      case "Enter":
      case " ":
        activate(node);
        break;
      case "F2":
        if (canEdit) onRename(node.file);
        else handled = false;
        break;
      case "Delete":
        if (canEdit) onDelete(node.file);
        else handled = false;
        break;
      case "m":
      case "M":
        if (canEdit && !event.ctrlKey && !event.metaKey && !event.altKey) onMove(node.file);
        else handled = false;
        break;
      default:
        handled = false;
    }
    if (handled) {
      event.preventDefault();
      event.stopPropagation();
    }
  }

  if (items.length === 0) {
    return <p className="px-2 py-1.5 text-rs-caption text-rs-text-muted">No files yet.</p>;
  }

  return (
    <>
      <ul
        role="tree"
        aria-label="Project files"
        aria-describedby={canEdit ? hintId : undefined}
        onKeyDown={onKeyDown}
        className="flex flex-col text-rs-body"
      >
        {tree.map((node) => (
          <Item key={node.file.id} node={node} focusId={focusId} {...props} />
        ))}
      </ul>
      {canEdit && (
        <p id={hintId} className="sr-only">
          F2 renames, M moves and Delete deletes the focused file or folder.
        </p>
      )}
    </>
  );
}

function Item({ node, focusId, ...props }: Props & { node: TreeNode; focusId: string | null }) {
  const { expanded, selectedId, openId, canEdit, onSelect, onToggle, onOpen, onRename, onMove, onDelete } = props;
  const { file, name, depth } = node;
  const isFolder = file.kind === "folder";
  const isOpen = isFolder && expanded.has(file.id);
  const selected = file.id === selectedId;
  const note = file.scanStatus === "pending" ? " (checking)" : file.scanStatus === "infected" ? " (removed: malware)" : "";

  return (
    <li
      id={treeItemId(file.id)}
      role="treeitem"
      aria-level={depth + 1}
      aria-selected={selected}
      aria-expanded={isFolder ? isOpen : undefined}
      aria-current={file.id === openId ? "page" : undefined}
      tabIndex={file.id === focusId ? 0 : -1}
      onClick={(e) => {
        e.stopPropagation();
        onSelect(file);
        if (isFolder) onToggle(file);
        else onOpen(file);
      }}
      className="rounded-control outline-none focus-visible:shadow-focus"
    >
      <div
        style={{ paddingLeft: `${8 + depth * 16}px` }}
        className={`flex cursor-pointer items-center justify-between gap-2 rounded-control py-1.5 pr-2 ${
          selected ? "bg-rs-accent-soft font-medium text-rs-heading" : "hover:bg-rs-subtle"
        }`}
      >
        <span className={`flex min-w-0 items-center gap-1 ${file.id === openId ? "font-medium" : ""}`}>
          {isFolder && (
            <svg aria-hidden="true" viewBox="0 0 24 24" className={`size-3.5 flex-none transition-transform ${isOpen ? "rotate-90" : ""}`}>
              <path d="M9 6l6 6-6 6" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" />
            </svg>
          )}
          <span className="truncate">
            {name}
            {isFolder ? "/" : ""}
            {note && <span className="text-rs-caption font-normal text-rs-text-muted">{note}</span>}
          </span>
          {!isFolder && file.path === props.mainFilePath && (
            // Hidden while the row shows its actions, so the name keeps its room; still read out.
            <span
              className={`flex-none rounded-rs-pill bg-rs-subtle px-1.5 text-rs-micro font-medium text-rs-text-muted ${selected && canEdit ? "sr-only" : ""}`}
            >
              <span className="sr-only">, </span>Main
            </span>
          )}
          {!isFolder && <Presence users={props.presence?.[file.id] ?? []} />}
        </span>
        {selected && canEdit && (
          <span className="flex flex-none items-center gap-1 text-rs-caption font-normal">
            <RowAction label={`Rename ${name}`} text="Rename" onClick={() => onRename(file)} />
            <span aria-hidden="true">·</span>
            <RowAction label={`Move ${name}`} text="Move" onClick={() => onMove(file)} />
            <span aria-hidden="true">·</span>
            <RowAction label={`Delete ${name}`} text="Delete" onClick={() => onDelete(file)} />
          </span>
        )}
      </div>
      {isOpen && node.children.length > 0 && (
        <ul role="group">
          {node.children.map((child) => (
            <Item key={child.file.id} node={child} focusId={focusId} {...props} />
          ))}
        </ul>
      )}
    </li>
  );
}

// Presence shows who has the file open to edit: a dot in each member's colour (up to three, then
// "+n"), their names in the tooltip and for screen readers.
function Presence({ users }: { users: PresenceUser[] }) {
  if (users.length === 0) return null;
  const names = users.map((u) => u.name || "A member").join(", ");
  const shown = users.slice(0, 3);
  return (
    <span title={`Editing: ${names}`} className="ml-1 flex flex-none items-center gap-0.5">
      {shown.map((u) => (
        <span key={u.id} aria-hidden="true" style={{ backgroundColor: u.colour }} className="size-2 rounded-full" />
      ))}
      {users.length > shown.length && (
        <span aria-hidden="true" className="text-rs-caption font-normal text-rs-text-muted">
          +{users.length - shown.length}
        </span>
      )}
      <span className="sr-only">, editing: {names}</span>
    </span>
  );
}

// RowAction is a mouse shortcut on the selected row; the keyboard has F2, M and Delete, so it
// is not a tab stop inside the tree.
function RowAction({ label, text, onClick }: { label: string; text: string; onClick: () => void }) {
  return (
    <button
      type="button"
      tabIndex={-1}
      aria-label={label}
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
      className="hover:text-rs-accent-strong hover:underline"
    >
      {text}
    </button>
  );
}
