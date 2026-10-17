"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

import { LogoMark } from "@/components/logo";

// Most pages behind these links do not exist yet; they link to "#" until they do.
const columns = [
  [
    { href: "#", label: "About" },
    { href: "#", label: "Accessibility" },
    { href: "#", label: "Help Center" },
  ],
  [
    { href: "#", label: "Privacy Policy" },
    { href: "#", label: "Terms of Use" },
    { href: "#", label: "Community Guidelines" },
  ],
  [
    { href: "#", label: "AI transparency" },
    { href: "#", label: "Contact" },
    { href: "#", label: "Careers" },
  ],
];

const linkClass = "rounded-sm hover:text-text hover:underline focus-visible:ring-2 focus-visible:ring-primary focus-visible:outline-none";

// CurrentAppFooter leaves the footer out of the editor workspace, which fills the screen.
// usePathname is only known at request time, so AppShell renders this inside <Suspense>.
export function CurrentAppFooter() {
  const pathname = usePathname() ?? "";
  return pathname.endsWith("/workspace") ? null : <AppFooter />;
}

// AppFooter is the signed-in footer at the bottom of the main column, inside the page
// width (docs/ui-guidelines.md, Signed-in pages).
export function AppFooter() {
  return (
    <footer className="mt-10 border-t border-border pt-6 text-xs text-text-muted">
      <nav aria-label="Footer">
        <div className="grid gap-4 sm:grid-cols-3">
          {columns.map((links, i) => (
            <ul key={i} className="space-y-2">
              {links.map((link) => (
                <li key={link.label}>
                  <Link href={link.href} className={linkClass}>
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>
          ))}
        </div>
      </nav>

      <ul className="mt-6 grid gap-4 sm:grid-cols-3">
        <HelpRow icon={<QuestionIcon />} title="Questions?">
          <Link href="#" className={linkClass}>
            Visit our Help Center
          </Link>
        </HelpRow>
        <HelpRow icon={<GearIcon />} title="Manage your account and privacy">
          <Link href="/settings/account" className={linkClass}>
            Go to your Settings
          </Link>
        </HelpRow>
        <HelpRow icon={<ShieldIcon />} title="AI transparency">
          <Link href="#" className={linkClass}>
            Learn how recommendations are made
          </Link>
        </HelpRow>
      </ul>

      <div className="mt-6 flex flex-wrap items-center justify-between gap-4">
        {/* The interface is in English only (SRS 2.5), so English is the one choice. */}
        <label className="flex items-center gap-2">
          <span>Language</span>
          <select
            defaultValue="en"
            className="rounded-lg border border-border bg-surface px-2 py-1 text-xs text-text focus-visible:ring-2 focus-visible:ring-primary focus-visible:outline-none"
          >
            <option value="en">English</option>
          </select>
        </label>
        <p className="flex items-center gap-2">
          <LogoMark className="h-4 w-4" />
          <span>ResearchSphere © 2026</span>
        </p>
      </div>
    </footer>
  );
}

function HelpRow({ icon, title, children }: { icon: ReactNode; title: string; children: ReactNode }) {
  return (
    <li className="flex items-start gap-2">
      <span className="text-text">{icon}</span>
      <span>
        <span className="block font-semibold text-text">{title}</span>
        {children}
      </span>
    </li>
  );
}

function Icon({ children }: { children: ReactNode }) {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="size-6 shrink-0"
    >
      {children}
    </svg>
  );
}

const QuestionIcon = () => (
  <Icon>
    <circle cx="12" cy="12" r="9" />
    <path d="M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.6.3-1 .9-1 1.6v.6" />
    <path d="M12 17h.01" />
  </Icon>
);

const GearIcon = () => (
  <Icon>
    <circle cx="12" cy="12" r="3" />
    <path d="M12 3v2.5M12 18.5V21M3 12h2.5M18.5 12H21M5.6 5.6l1.8 1.8M16.6 16.6l1.8 1.8M5.6 18.4l1.8-1.8M16.6 7.4l1.8-1.8" />
  </Icon>
);

const ShieldIcon = () => (
  <Icon>
    <path d="M12 3.5 5 6v5.5c0 4.3 3 7.7 7 9 4-1.3 7-4.7 7-9V6z" />
    <path d="m9 12 2 2 4-4" />
  </Icon>
);
