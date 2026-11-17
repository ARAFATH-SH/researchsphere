"use client";

import { defaultKeymap, history, historyKeymap, indentWithTab, redo, undo } from "@codemirror/commands";
import { bracketMatching, HighlightStyle, indentOnInput, StreamLanguage, syntaxHighlighting } from "@codemirror/language";
import { stex } from "@codemirror/legacy-modes/mode/stex";
import { highlightSelectionMatches, openSearchPanel, search, searchKeymap } from "@codemirror/search";
import { Annotation, Compartment, EditorState, type Extension, Prec } from "@codemirror/state";
import {
  drawSelection,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
} from "@codemirror/view";
import { tags } from "@lezer/highlight";
import { useEffect, useRef } from "react";

// The LaTeX editor (FR-12.3): CodeMirror 6 with the stex mode of @codemirror/legacy-modes,
// line numbers, undo and redo, find and replace (Ctrl/Cmd+F), bracket matching and soft wrap.
// This module and CodeMirror load only on the workspace route (next/dynamic in file-pane.tsx).

// TextSource is the document the editor shows. The workspace passes its Autosaver; the editor
// tells it about every change and never keeps the text anywhere else.
export interface TextSource {
  readonly text: string;
  edit(read: () => string): void;
}

// EditorHandle is what the toolbar and the conflict dialog can do with the editor.
export type EditorHandle = {
  undo: () => void;
  redo: () => void;
  openFind: () => void;
  // replaceText shows text that came from the server; it is not a change to save.
  replaceText: (text: string) => void;
  focus: () => void;
};

const fromServer = Annotation.define<boolean>();

// binding connects the document to its source: CodeMirror's own history, and each change sent
// to the Autosaver. Slice 22 replaces this one function with yCollab (y-codemirror.next) and
// the Yjs undo manager; nothing else in the editor depends on where the text lives.
function binding(source: TextSource): Extension[] {
  return [
    history(),
    keymap.of(historyKeymap),
    EditorView.updateListener.of((update) => {
      if (!update.docChanged || update.transactions.some((tr) => tr.annotation(fromServer))) return;
      const doc = update.state.doc;
      source.edit(() => doc.toString());
    }),
  ];
}

// The colours are the design's (design/screens/Workspace.html), from the --rs-* tokens.
const latexHighlight = HighlightStyle.define([
  { tag: tags.tagName, color: "var(--rs-heading)", fontWeight: "600" },
  { tag: [tags.atom, tags.keyword], color: "var(--rs-orange)" },
  { tag: tags.comment, color: "var(--rs-text-muted)" },
  { tag: tags.number, color: "var(--rs-info)" },
  { tag: tags.special(tags.variableName), color: "var(--rs-violet)" },
  { tag: tags.invalid, color: "var(--rs-danger)" },
]);

const theme = EditorView.theme({
  "&": { height: "100%", fontSize: "var(--rs-size-caption)", color: "var(--rs-text)", backgroundColor: "var(--rs-surface)" },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": { fontFamily: "var(--rs-font-mono)", lineHeight: "22px" },
  ".cm-content": { padding: "8px 0" },
  ".cm-gutters": { backgroundColor: "var(--rs-surface)", color: "var(--rs-text-muted)", border: "none" },
  ".cm-lineNumbers .cm-gutterElement": { minWidth: "32px", paddingRight: "12px" },
  ".cm-activeLine, .cm-activeLineGutter": { backgroundColor: "var(--rs-subtle)" },
  "&.cm-focused .cm-matchingBracket": { backgroundColor: "var(--rs-accent-soft)", outline: "1px solid var(--rs-accent-line)" },
  ".cm-searchMatch": { backgroundColor: "var(--rs-warning-soft)", outline: "1px solid var(--rs-warning-line)" },
  ".cm-searchMatch.cm-searchMatch-selected": { backgroundColor: "var(--rs-accent-soft)" },
  ".cm-panels": { backgroundColor: "var(--rs-subtle)", color: "var(--rs-text)", fontFamily: "var(--rs-font-sans)" },
  ".cm-panels.cm-panels-top": { borderBottom: "1px solid var(--rs-border)" },
  ".cm-panel.cm-search input, .cm-panel.cm-search button": { fontSize: "var(--rs-size-caption)" },
  ".cm-textfield": { border: "1px solid var(--rs-control)", borderRadius: "var(--rs-radius-control)" },
  ".cm-button": { backgroundImage: "none", border: "1px solid var(--rs-control)", borderRadius: "var(--rs-radius-pill)" },
});

function readOnlyExtensions(readOnly: boolean): Extension {
  return [
    EditorState.readOnly.of(readOnly),
    EditorView.editable.of(!readOnly),
    EditorView.contentAttributes.of(readOnly ? { "aria-readonly": "true" } : {}),
  ];
}

export type LatexEditorProps = {
  source: TextSource;
  label: string;
  readOnly: boolean;
  wrap: boolean;
  // onSave runs on Ctrl/Cmd+S, which never opens the browser's save dialog here.
  onSave: () => void;
  onReady?: (handle: EditorHandle | null) => void;
};

export default function LatexEditor({ source, label, readOnly, wrap, onSave, onReady }: LatexEditorProps) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const readOnlyConf = useRef(new Compartment());
  const wrapConf = useRef(new Compartment());
  const labelConf = useRef(new Compartment());
  const save = useRef(onSave);
  const ready = useRef(onReady);
  const first = useRef({ label, readOnly, wrap });

  useEffect(() => {
    save.current = onSave;
    ready.current = onReady;
  });

  // One view per source: a new file is a new source, and the view starts from its text.
  useEffect(() => {
    if (!host.current) return;
    const { label: ariaLabel, readOnly: ro, wrap: wr } = first.current;
    const v = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: source.text,
        extensions: [
          Prec.highest(keymap.of([{ key: "Mod-s", preventDefault: true, run: () => (save.current(), true) }])),
          lineNumbers(),
          highlightActiveLineGutter(),
          highlightActiveLine(),
          drawSelection(),
          indentOnInput(),
          bracketMatching(),
          highlightSelectionMatches(),
          search({ top: true }),
          StreamLanguage.define(stex),
          syntaxHighlighting(latexHighlight),
          binding(source),
          keymap.of([...searchKeymap, ...defaultKeymap, indentWithTab]),
          labelConf.current.of(EditorView.contentAttributes.of({ "aria-label": ariaLabel })),
          readOnlyConf.current.of(readOnlyExtensions(ro)),
          wrapConf.current.of(wr ? EditorView.lineWrapping : []),
          theme,
        ],
      }),
    });
    view.current = v;
    ready.current?.({
      undo: () => undo(v),
      redo: () => redo(v),
      openFind: () => openSearchPanel(v),
      replaceText: (text) =>
        v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: text }, annotations: fromServer.of(true) }),
      focus: () => v.focus(),
    });
    return () => {
      ready.current?.(null);
      view.current = null;
      v.destroy();
    };
  }, [source]);

  useEffect(() => {
    view.current?.dispatch({ effects: readOnlyConf.current.reconfigure(readOnlyExtensions(readOnly)) });
  }, [readOnly]);

  useEffect(() => {
    view.current?.dispatch({ effects: labelConf.current.reconfigure(EditorView.contentAttributes.of({ "aria-label": label })) });
  }, [label]);

  useEffect(() => {
    view.current?.dispatch({ effects: wrapConf.current.reconfigure(wrap ? EditorView.lineWrapping : []) });
  }, [wrap]);

  return <div ref={host} className="h-full min-h-0" data-testid="latex-editor" />;
}
