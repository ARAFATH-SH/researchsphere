import type { ReactNode } from "react";

// The design's message boxes on the auth cards (design/screens/Auth.html): 8px radius,
// 10px by 12px padding, 14px text.
export type NoticeTone = "error" | "success" | "warning";

const tones: Record<NoticeTone, string> = {
  error: "border border-rs-danger-line bg-rs-danger-soft text-rs-danger-strong",
  success: "bg-rs-accent-soft text-rs-heading",
  warning: "border border-rs-warning-line bg-rs-warning-soft text-rs-warning",
};

export function noticeClass(tone: NoticeTone): string {
  return `rounded-rs-md px-3 py-2.5 text-rs-body ${tones[tone]}`;
}

// AuthAlert is an error that belongs to no single field (role="alert").
export function AuthAlert({ children }: { children: ReactNode }) {
  return (
    <p role="alert" className={noticeClass("error")}>
      {children}
    </p>
  );
}

// StatusNotice is a live region that stays mounted so screen readers announce what is put in
// it. Empty, it takes no space; with text it is a box in the given tone.
export function StatusNotice({ tone = "success", children }: { tone?: NoticeTone; children?: ReactNode }) {
  return (
    <p role="status" className={children ? noticeClass(tone) : "sr-only"}>
      {children ?? null}
    </p>
  );
}

// noticeLinkClass is a link or link-styled button inside a notice: the notice's color, 500.
export const noticeLinkClass =
  "font-medium text-inherit underline hover:no-underline focus-visible:shadow-focus focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60";
