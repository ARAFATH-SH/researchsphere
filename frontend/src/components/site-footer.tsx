"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { Container } from "@/components/Container";
import { LogoSquare } from "@/components/logo";
import { ApiStatus } from "@/features/health/api-status";

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
      { href: "/connections", label: "Find collaborators" },
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

const linkFocus = "hover:text-rs-accent hover:underline focus-visible:text-rs-accent focus-visible:underline focus-visible:outline-none";
const linkClass = `text-rs-footer-link ${linkFocus}`;
const columnClass = "flex flex-col gap-1.5 border-l border-rs-nav-border px-4 max-[960px]:border-l-0 max-[960px]:p-0";

// CurrentSiteFooter leaves the footer out of the editor workspace, which fills the screen.
// usePathname is only known at request time, so AppShell renders this inside <Suspense>.
export function CurrentSiteFooter() {
  const pathname = usePathname() ?? "";
  return pathname.endsWith("/workspace") ? null : <SiteFooter />;
}

// SiteFooter is the footer of every page but the workspace, signed in or not
// (docs/ui-guidelines.md, Footer): the brand, three link columns, help links and the
// language. Six columns, three at 960px and narrower, two at 560px and narrower.
export function SiteFooter() {
  return (
    <footer
      aria-label="Site footer"
      className="mt-auto border-t border-rs-nav-border bg-rs-footer-bg font-rs-ui text-rs-caption leading-rs-relaxed text-rs-text"
    >
      <Container className="grid grid-cols-[minmax(150px,1.1fr)_repeat(3,minmax(110px,1fr))_minmax(190px,1.4fr)_minmax(150px,1fr)] gap-y-4 py-6 max-[960px]:grid-cols-3 max-[960px]:gap-4 max-[560px]:grid-cols-2">
        <div className="pr-4 max-[960px]:col-span-full max-[960px]:p-0">
          <Link href="/" className={`inline-flex items-center gap-1.5 text-rs-body font-bold text-rs-accent ${linkFocus}`}>
            <LogoSquare size="sm" />
            ResearchSphere
          </Link>
          <p className="mt-1.5 text-rs-footer-muted">ResearchSphere © 2026</p>
          <div className="mt-1.5 text-rs-footer-muted">
            <ApiStatus />
          </div>
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
