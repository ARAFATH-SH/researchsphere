import { type ReactNode, Suspense } from "react";

import { CurrentAppFooter } from "@/components/app-footer";
import { Container } from "@/components/Container";
import { Logo } from "@/components/logo";

import { SearchIcon } from "./icons";
import { CurrentTabList, TabList, type TabListVariant } from "./tab-list";

// AppShell is the signed-in frame: a fixed white top bar with the logo, search and icon
// tabs, below 768px a fixed bottom bar with the same tabs, and the signed-in footer under
// the page (docs/ui-guidelines.md).
export function AppShell({ children }: { children: ReactNode }) {
  return (
    <>
      <header className="fixed inset-x-0 top-0 z-20 border-b border-border bg-surface">
        <Container size="app" className="flex h-14 items-center gap-3">
          <Logo href="/home" compactBelow="lg" />
          <SearchBox />
          <nav aria-label="Main" className="ml-auto hidden h-full md:block">
            <Tabs variant="top" />
          </nav>
        </Container>
      </header>

      <Container as="main" size="app" className="flex-1 pt-20 pb-24 md:pb-10">
        {children}
        <Suspense fallback={null}>
          <CurrentAppFooter />
        </Suspense>
      </Container>

      <nav aria-label="Main" className="fixed inset-x-0 bottom-0 z-20 border-t border-border bg-surface md:hidden">
        <Tabs variant="bottom" />
      </nav>
    </>
  );
}

// Tabs prerenders the tab list with no tab marked, then marks the current one.
function Tabs({ variant }: { variant: TabListVariant }) {
  return (
    <Suspense fallback={<TabList pathname="" variant={variant} />}>
      <CurrentTabList variant={variant} />
    </Suspense>
  );
}

// SearchBox sends the query to the search page (FR-6), which a later slice builds.
function SearchBox() {
  return (
    <form action="/search" role="search" className="relative min-w-0 flex-1 md:max-w-72">
      <label htmlFor="site-search" className="sr-only">
        Search
      </label>
      <span className="pointer-events-none absolute inset-y-0 left-3 flex items-center text-text-muted">
        <SearchIcon />
      </span>
      <input
        id="site-search"
        name="q"
        type="search"
        placeholder="Search"
        className="h-9 w-full rounded-lg border border-transparent bg-primary-soft pr-3 pl-9 text-sm text-text placeholder:text-text-muted focus:border-primary focus:bg-surface focus:outline-none"
      />
    </form>
  );
}
