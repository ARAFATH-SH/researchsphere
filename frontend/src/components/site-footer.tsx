"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { Container } from "@/components/Container";
import { LogoSquare } from "@/components/logo";

// Most pages behind these links do not exist yet; they link to "#" until they do.
const columns = [
  {
    title: "About",
    links: [
      { href: "#", label: "About ResearchSphere" },
      { href: "#", label: "Community guidelines" },
      { href: "#", label: "Privacy & terms" },
      { href: "#", label: "Accessibility" },
    ],
  },
  {
    title: "For researchers",
    links: [
      { href: "/network", label: "Find collaborators" },
      { href: "/projects", label: "Research projects" },
      { href: "/events", label: "Events & venues" },
      // The workspace belongs to a project, so there is no page to link to yet.
      { href: "#", label: "LaTeX workspace" },
    ],
  },
  {
    title: "Platform",
    links: [
      { href: "#", label: "How AI matching works" },
      { href: "#", label: "Publications & ORCID" },
      { href: "#", label: "Safety center" },
      { href: "#", label: "Developers" },
    ],
  },
];

const help = [
  { href: "#", label: "Questions?", sub: "Visit our Help Center." },
  { href: "/settings/account", label: "Manage your account and privacy", sub: "Go to your Settings." },
  { href: "#", label: "Report a problem", sub: "Content, safety and bug reports." },
];

// The compact footer's links (design/screens/Home.html, .rs-footer-compact).
const compactLinks = [
  { href: "#", label: "About" },
  { href: "#", label: "Accessibility" },
  { href: "#", label: "Help Center" },
  { href: "#", label: "Privacy & terms" },
  { href: "/settings/account", label: "Settings" },
  { href: "#", label: "Safety center" },
];

const linkFocus = "hover:text-rs-accent hover:underline focus-visible:text-rs-accent focus-visible:underline focus-visible:outline-none";
const linkClass = `text-rs-footer-link ${linkFocus}`;
const columnClass = "flex flex-col gap-1.5 border-l border-rs-nav-border px-4 max-[960px]:border-l-0 max-[960px]:p-0";

export type FooterVariant = "full" | "compact" | "none";

// footerFor is the one place that says which footer a signed-in page has (design/screens):
// the home feed and My Network put the compact footer in their right column, messaging and
// the editor workspace fill the screen and have none, and every other page ends with the
// full footer.
export function footerFor(pathname: string): FooterVariant {
  if (pathname === "/home" || pathname === "/network") return "compact";
  if (pathname === "/messaging" || pathname.startsWith("/messaging/") || pathname.endsWith("/workspace")) {
    return "none";
  }
  return "full";
}

// CurrentSiteFooter is the footer AppShell puts after main: the full footer where footerFor
// says "full", nothing otherwise (the home page places its compact footer itself).
// usePathname is only known at request time, so AppShell renders this inside <Suspense>.
export function CurrentSiteFooter() {
  const pathname = usePathname() ?? "";
  return footerFor(pathname) === "full" ? <SiteFooter /> : null;
}

// SiteFooter is the site footer (docs/ui-guidelines.md, Site footer). "full" spans the page:
// the brand, three link columns, help links and the language; six columns, three at 960px
// and narrower, two at 560px and narrower. "compact" is the bordered card at the bottom of
// the home page's right column: centred links and the copyright. The full footer keeps 16px
// side padding under 640px, as the design's footer does (the page gutter is 12px there).
export function SiteFooter({ variant = "full" }: { variant?: Exclude<FooterVariant, "none"> }) {
  if (variant === "compact") {
    return (
      <footer
        aria-label="Site footer"
        className="rounded-card border border-rs-nav-border bg-rs-footer-bg font-rs-ui text-rs-caption leading-rs-relaxed text-rs-text"
      >
        <div className="flex flex-wrap justify-center gap-x-3 gap-y-1 p-3 text-center">
          {compactLinks.map((link) => (
            <Link key={link.label} href={link.href} className={linkClass}>
              {link.label}
            </Link>
          ))}
          <span className="mt-1 flex w-full items-center justify-center gap-1.5">
            <LogoSquare size="sm" />
            <span className="text-rs-footer-muted">ResearchSphere © 2026</span>
          </span>
        </div>
      </footer>
    );
  }
  return (
    <footer
      aria-label="Site footer"
      className="mt-auto border-t border-rs-nav-border bg-rs-footer-bg font-rs-ui text-rs-caption leading-rs-relaxed text-rs-text"
    >
      <Container className="grid grid-cols-[minmax(150px,1.1fr)_repeat(3,minmax(110px,1fr))_minmax(190px,1.4fr)_minmax(150px,1fr)] gap-y-4 py-6 max-[640px]:px-4 max-[960px]:grid-cols-3 max-[960px]:gap-4 max-[560px]:grid-cols-2">
        <div className="pr-4 max-[960px]:col-span-full max-[960px]:p-0">
          <Link href="/" className={`inline-flex items-center gap-1.5 text-rs-body font-bold text-rs-accent ${linkFocus}`}>
            <LogoSquare size="sm" />
            ResearchSphere
          </Link>
          <p className="mt-1.5 text-rs-footer-muted">ResearchSphere © 2026</p>
        </div>

        {columns.map((column) => (
          <nav key={column.title} aria-label={column.title} className={columnClass}>
            <span className="mb-0.5 font-medium">{column.title}</span>
            {column.links.map((link) => (
              <Link key={link.label} href={link.href} className={linkClass}>
                {link.label}
              </Link>
            ))}
          </nav>
        ))}

        <div className={`${columnClass} gap-2.5`}>
          {help.map((row) => (
            <span key={row.label}>
              <Link href={row.href} className={`font-medium text-rs-text ${linkFocus}`}>
                {row.label}
              </Link>
              <span className="block text-rs-footer-muted">{row.sub}</span>
            </span>
          ))}
        </div>

        <div className={columnClass}>
          <label htmlFor="site-language" className="mb-1 block text-rs-footer-muted">
            Select language
          </label>
          {/* Shown only: the interface is in English only (SRS 2.5). */}
          <select
            id="site-language"
            defaultValue="en"
            className="h-8 w-full rounded-rs-xs border border-rs-control bg-rs-footer-bg px-1.5 text-rs-text focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-rs-accent"
          >
            <option value="en">English (English)</option>
          </select>
        </div>
      </Container>
    </footer>
  );
}
