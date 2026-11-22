import { Annotation, type Extension, type Range, RangeSet } from "@codemirror/state";
import { Decoration, type DecorationSet, EditorView, type PluginValue, ViewPlugin, type ViewUpdate, WidgetType } from "@codemirror/view";
import type { Awareness } from "y-protocols/awareness";
import * as Y from "yjs";

import { safeColour } from "./types";

// Other members' cursors and selections, each with the member's name in their colour (FR-12.5),
// as design/screens/Workspace.html draws them: a 2 px caret with the name on a flag above it.
// It also publishes this member's own cursor as awareness. Adapted from y-codemirror.next's
// yRemoteSelections (MIT), which reads `user.color`; the sync server sends `user.colour` and
// sets the name and colour itself (docs/sync.md), so the client cannot choose either.

const refresh = Annotation.define<true>();

type Cursor = { anchor: unknown; head: unknown };

class CaretWidget extends WidgetType {
  constructor(
    readonly colour: string,
    readonly name: string,
  ) {
    super();
  }

  toDOM(): HTMLElement {
    const caret = document.createElement("span");
    caret.className = "cm-rs-caret";
    caret.style.backgroundColor = this.colour;
    caret.setAttribute("aria-hidden", "true");
    const label = document.createElement("span");
    label.className = "cm-rs-caret-label";
    label.style.backgroundColor = this.colour;
    label.textContent = this.name;
    caret.append("⁠", label, "⁠");
    return caret;
  }

  eq(other: CaretWidget): boolean {
    return other.colour === this.colour && other.name === this.name;
  }

  ignoreEvent(): boolean {
    return true;
  }
}

const theme = EditorView.baseTheme({
  ".cm-rs-caret": {
    position: "relative",
    display: "inline-block",
    width: "2px",
    marginLeft: "-1px",
    marginRight: "-1px",
    height: "1.2em",
    verticalAlign: "text-bottom",
  },
  ".cm-rs-caret-label": {
    position: "absolute",
    bottom: "100%",
    left: "0",
    padding: "1px 5px",
    borderRadius: "3px 3px 3px 0",
    color: "var(--rs-surface)",
    fontFamily: "var(--rs-font-sans)",
    fontSize: "11px",
    fontWeight: "500",
    lineHeight: "normal",
    whiteSpace: "nowrap",
    userSelect: "none",
    pointerEvents: "none",
    zIndex: "10",
  },
});

export function remoteCursors(text: Y.Text, awareness: Awareness): Extension {
  const doc = text.doc!;

  class Plugin implements PluginValue {
    decorations: DecorationSet = RangeSet.empty;
    private readonly onChange: (change: { added: number[]; updated: number[]; removed: number[] }) => void;
    private destroyed = false;

    constructor(private readonly view: EditorView) {
      this.onChange = ({ added, updated, removed }) => {
        const others = [...added, ...updated, ...removed].some((id) => id !== doc.clientID);
        if (others) queueMicrotask(() => !this.destroyed && this.view.dispatch({ annotations: refresh.of(true) }));
      };
      awareness.on("change", this.onChange);
      this.decorations = this.build(view);
    }

    destroy(): void {
      this.destroyed = true;
      awareness.off("change", this.onChange);
    }

    update(update: ViewUpdate): void {
      this.publish(update);
      if (update.docChanged || update.transactions.some((tr) => tr.annotation(refresh))) {
        this.decorations = this.build(update.view);
      }
    }

    /** This member's cursor, as positions that follow the text (relative positions). */
    private publish(update: ViewUpdate): void {
      const local = awareness.getLocalState();
      if (local === null) return;
      const focused = update.view.hasFocus && update.view.dom.ownerDocument.hasFocus();
      const current = local.cursor as Cursor | null | undefined;
      if (!focused) return;
      const sel = update.state.selection.main;
      const anchor = Y.createRelativePositionFromTypeIndex(text, sel.anchor);
      const head = Y.createRelativePositionFromTypeIndex(text, sel.head);
      const same =
        current?.anchor &&
        current.head &&
        Y.compareRelativePositions(Y.createRelativePositionFromJSON(current.anchor), anchor) &&
        Y.compareRelativePositions(Y.createRelativePositionFromJSON(current.head), head);
      if (!same) awareness.setLocalStateField("cursor", { anchor, head });
    }

    private build(view: EditorView): DecorationSet {
      const decorations: Range<Decoration>[] = [];
      const length = view.state.doc.length;
      awareness.getStates().forEach((state, clientId) => {
        if (clientId === doc.clientID) return;
        const cursor = state.cursor as Cursor | null | undefined;
        const user = state.user as { name?: unknown; colour?: unknown } | undefined;
        const colour = safeColour(user?.colour);
        if (!cursor?.anchor || !cursor.head || !colour) return;
        const anchor = Y.createAbsolutePositionFromRelativePosition(Y.createRelativePositionFromJSON(cursor.anchor), doc);
        const head = Y.createAbsolutePositionFromRelativePosition(Y.createRelativePositionFromJSON(cursor.head), doc);
        if (!anchor || !head || anchor.type !== text || head.type !== text) return;
        const from = Math.min(anchor.index, head.index, length);
        const to = Math.min(Math.max(anchor.index, head.index), length);
        if (from < to) {
          decorations.push(
            Decoration.mark({ class: "cm-rs-selection", attributes: { style: `background-color: ${colour}33` } }).range(from, to),
          );
        }
        const name = typeof user?.name === "string" && user.name ? user.name : "Member";
        const at = Math.min(head.index, length);
        decorations.push(
          Decoration.widget({ widget: new CaretWidget(colour, name), side: head.index > anchor.index ? -1 : 1 }).range(at),
        );
      });
      return Decoration.set(decorations, true);
    }
  }

  return [theme, ViewPlugin.fromClass(Plugin, { decorations: (p) => p.decorations })];
}
