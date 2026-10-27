"use client";

import { type ReactNode, useEffect, useState } from "react";

// The settings and admin pages share one layout (design/screens Settings and Admin): a card
// of section links on the left and a column of section cards on the right. Both wrap under
// each other when the row is too narrow, as on a phone.

export type SectionLink = { id: string; label: string };

type Variant = "settings" | "admin";

// The column widths and link padding differ a little between the two designs.
const variants: Record<Variant, { nav: string; link: string; column: string }> = {
  settings: { nav: "flex-[1_1_220px] max-w-[260px]", link: "py-3", column: "flex-[999_1_560px]" },
  admin: { nav: "flex-[1_1_200px] max-w-[240px]", link: "py-[11px]", column: "flex-[999_1_600px]" },
};

const cardClass = "rounded-card border bg-rs-surface shadow-card";

// The nav sticks under the 52px top bar only where it sits beside the column (from 836px:
// 220 + 24 + 560 and the two 16px gutters). Once they wrap, a sticky nav would cover the cards.
const stickyClass = "min-[836px]:sticky min-[836px]:top-[76px]";

type SectionLayoutProps = {
  title: string;
  navLabel: string;
  sections: SectionLink[];
  variant?: Variant;
  children: ReactNode;
};

// SectionLayout marks the section in view: the first at the start, the one whose link was
// clicked, and, where the browser has IntersectionObserver, the one scrolled to.
export function SectionLayout({ title, navLabel, sections, variant = "settings", children }: SectionLayoutProps) {
  const [active, setActive] = useState(sections[0]?.id);
  const styles = variants[variant];

  useEffect(() => {
    if (typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      (entries) => {
        const seen = entries.filter((e) => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
        if (seen[0]) setActive(seen[0].target.id);
      },
      // A section counts as in view when it reaches the top third of the screen, under the bar.
      { rootMargin: "-76px 0px -66% 0px" },
    );
    for (const { id } of sections) {
      const el = document.getElementById(id);
      if (el) observer.observe(el);
    }
    return () => observer.disconnect();
  }, [sections]);

  return (
    <div className="flex flex-wrap items-start gap-6 max-[640px]:gap-4">
      <nav
        aria-label={navLabel}
        className={`${cardClass} min-w-0 overflow-hidden border-rs-border ${styles.nav} ${stickyClass} max-[640px]:max-w-none`}
      >
        <div className="border-b border-rs-divider px-4 py-3.5 text-rs-lead font-medium text-rs-text">{title}</div>
        {sections.map((s) => {
          const current = s.id === active;
          return (
            <a
              key={s.id}
              href={`#${s.id}`}
              aria-current={current ? "true" : undefined}
              onClick={() => setActive(s.id)}
              className={`block px-4 ${styles.link} text-rs-body no-underline focus-visible:shadow-focus focus-visible:outline-none ${
                current ? "bg-rs-accent-soft font-medium text-rs-heading" : "text-rs-text hover:text-rs-accent-hover"
              }`}
            >
              {s.label}
            </a>
          );
        })}
      </nav>
      <div className={`flex min-w-0 flex-col gap-4 ${styles.column}`}>{children}</div>
    </div>
  );
}

type SectionCardProps = {
  id: string;
  title: string;
  // tone "danger" is the card for an action that destroys data, such as deleting the account.
  tone?: "default" | "danger";
  // header puts the title in a bar with a divider under it, for a card that holds a table;
  // otherwise the card has 20px padding and its parts are 12px apart.
  header?: boolean;
  // aside sits at the right end of the title row, such as a count.
  aside?: ReactNode;
  children: ReactNode;
};

// SectionCard is one section of a SectionLayout. Its id is what the nav links to; the scroll
// margin keeps its title clear of the sticky top bar.
export function SectionCard({ id, title, tone = "default", header = false, aside, children }: SectionCardProps) {
  const danger = tone === "danger";
  const titleRow = (
    <div className={`flex flex-wrap items-center justify-between gap-2 ${header ? "border-b border-rs-divider px-4 py-3.5" : ""}`}>
      <h2
        id={`${id}-title`}
        className={`text-rs-lead leading-rs-snug font-medium ${danger ? "text-rs-danger-strong" : "text-rs-text"}`}
      >
        {title}
      </h2>
      {aside}
    </div>
  );
  return (
    <section
      id={id}
      aria-labelledby={`${id}-title`}
      className={`${cardClass} scroll-mt-[76px] ${danger ? "border-rs-danger-line" : "border-rs-border"} ${
        header ? "" : "flex flex-col gap-3 p-5"
      }`}
    >
      {titleRow}
      {children}
    </section>
  );
}
