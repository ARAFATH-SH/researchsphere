"use client";

import { type ReactNode, useEffect, useId, useRef, useState } from "react";

// longPress is how long a finger stays on a message before its menu opens.
const longPress = 500;

type MessageMenuProps = {
  messageId: string;
  onReport: (messageId: string) => void;
  children: ReactNode;
};

// MessageMenu wraps one of the other person's messages with a "…" that shows on hover and on
// keyboard focus, and offers "Report message". On a touch screen, where nothing hovers, a long
// press opens it. A scroll cancels the press.
export function MessageMenu({ messageId, onReport, children }: MessageMenuProps) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const item = useRef<HTMLButtonElement>(null);
  const press = useRef<ReturnType<typeof setTimeout>>(undefined);
  const menuId = useId();

  useEffect(() => () => clearTimeout(press.current), []);

  useEffect(() => {
    if (!open) return;
    item.current?.focus();
    function onPointer(event: MouseEvent) {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    }
    document.addEventListener("mousedown", onPointer);
    return () => document.removeEventListener("mousedown", onPointer);
  }, [open]);

  const cancelPress = () => clearTimeout(press.current);

  return (
    <div
      ref={root}
      className="group/message relative flex items-center gap-1"
      onTouchStart={() => {
        cancelPress();
        press.current = setTimeout(() => setOpen(true), longPress);
      }}
      onTouchEnd={cancelPress}
      onTouchMove={cancelPress}
      onTouchCancel={cancelPress}
    >
      {children}
      <button
        ref={button}
        type="button"
        aria-label="More actions for this message"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={menuId}
        onClick={() => setOpen(!open)}
        className={`flex-none rounded-full p-1 text-rs-text-muted hover:bg-rs-subtle hover:text-rs-text focus-visible:opacity-100 focus-visible:shadow-focus focus-visible:outline-none group-hover/message:opacity-100 ${
          open ? "opacity-100" : "opacity-0"
        }`}
      >
        <svg aria-hidden="true" viewBox="0 0 24 24" fill="currentColor" className="size-4">
          <circle cx="5" cy="12" r="1.75" />
          <circle cx="12" cy="12" r="1.75" />
          <circle cx="19" cy="12" r="1.75" />
        </svg>
      </button>
      <div
        id={menuId}
        role="menu"
        aria-label="Message actions"
        hidden={!open}
        onKeyDown={(event) => {
          if (event.key !== "Escape" && event.key !== "Tab") return;
          if (event.key === "Escape") event.preventDefault();
          setOpen(false);
          button.current?.focus();
        }}
        className="absolute top-full left-0 z-30 mt-1 w-44 rounded-rs-md border border-rs-border bg-rs-surface py-1 shadow-menu"
      >
        <button
          ref={item}
          type="button"
          role="menuitem"
          onClick={() => {
            setOpen(false);
            onReport(messageId);
          }}
          className="w-full px-4 py-2 text-left text-rs-body text-rs-text hover:bg-rs-subtle focus-visible:bg-rs-subtle focus-visible:outline-none"
        >
          Report message
        </button>
      </div>
    </div>
  );
}
