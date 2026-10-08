import Link from "next/link";

const links = [
  { href: "/home", label: "Home" },
  { href: "/search", label: "Search" },
  { href: "/projects", label: "Projects" },
  { href: "/events", label: "Events" },
  { href: "/connections", label: "Connections" },
  { href: "/notifications", label: "Notifications" },
  { href: "/profile", label: "Profile" },
  { href: "/admin", label: "Admin" },
  { href: "/login", label: "Log in" },
];

// SiteHeader shows every page until the auth slice decides which links each role sees.
// The links wrap onto a second line on narrow screens (SRS 5.1: usable at 768 pixels).
export function SiteHeader() {
  return (
    <header className="border-b border-gray-200 dark:border-gray-800">
      <div className="mx-auto flex w-full max-w-5xl flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-3">
        <Link href="/" className="text-lg font-semibold">
          ResearchSphere
        </Link>
        <nav aria-label="Main">
          <ul className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
            {links.map(({ href, label }) => (
              <li key={href}>
                <Link href={href} className="hover:underline">
                  {label}
                </Link>
              </li>
            ))}
          </ul>
        </nav>
      </div>
    </header>
  );
}
