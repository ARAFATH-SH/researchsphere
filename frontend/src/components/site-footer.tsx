import Link from "next/link";

import { Container } from "@/components/Container";
import { ApiStatus } from "@/features/health/api-status";

// The pages behind these links do not exist yet.
const links = [
  { href: "#", label: "About" },
  { href: "#", label: "Privacy" },
  { href: "#", label: "Terms" },
  { href: "#", label: "Contact" },
];

// SiteFooter is the signed-out footer: links, the copyright and the API status
// (docs/ui-guidelines.md, Signed-out pages).
export function SiteFooter() {
  return (
    <footer className="mt-auto border-t border-border bg-surface">
      <Container
        size="marketing"
        className="flex flex-col gap-4 py-8 text-sm text-text-muted md:flex-row md:items-center md:justify-between"
      >
        <nav aria-label="Footer">
          <ul className="flex flex-wrap gap-x-6 gap-y-2">
            {links.map((link) => (
              <li key={link.label}>
                <Link href={link.href} className="hover:text-text hover:underline">
                  {link.label}
                </Link>
              </li>
            ))}
          </ul>
        </nav>
        <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
          <span>© ResearchSphere</span>
          <ApiStatus />
        </div>
      </Container>
    </footer>
  );
}
