import Link from "next/link";
import { type ReactNode, Suspense } from "react";

import { Container } from "@/components/Container";
import { Logo } from "@/components/logo";
import { CurrentSiteFooter } from "@/components/site-footer";

import { MessagingIcon, SearchIcon } from "./icons";
import { MeMenu } from "./me-menu";
import { CurrentTabList, TabList, type TabListVariant } from "./tab-list";

// AppShell is the signed-in frame (docs/ui-guidelines.md, Signed-in pages): a sticky 52px
// top bar with the logo, search and icon tabs, whose tabs move to a 56px bottom bar from 481
// to 640px. At 480px and narrower a phone top bar (avatar, search, messages) and a 64px
// bottom bar replace it. The site footer follows the page.
export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex flex-1 flex-col max-[640px]:pb-14 max-[480px]:pt-14 max-[480px]:pb-16">
      <header className="sticky top-0 z-50 h-[52px] border-b border-rs-nav-border bg-rs-surface font-rs-ui text-rs-nav-text max-[480px]:hidden">
        <Container className="flex h-[52px] items-center gap-2 max-[640px]:px-3">
          <Logo href="/home" markOnly />
          <SearchBox id="site-search" variant="top" />
          <nav aria-label="Main" className="ml-auto flex h-full">
            <Tabs variant="top" />
          </nav>
        </Container>
      </header>

      <header className="fixed inset-x-0 top-0 z-[70] hidden h-14 items-center gap-3 border-b border-rs-nav-border bg-rs-surface px-3 font-rs-ui text-rs-nav-text shadow-rs-bar max-[480px]:flex">
        <MeMenu placement="phone" />
        <SearchBox id="phone-search" variant="phone" />
        <Link
          href="/messaging"
          aria-label="Messaging"
          className="relative flex size-11 flex-none items-center justify-center rounded-full text-rs-nav-muted focus-visible:shadow-focus focus-visible:outline-none"
        >
          <MessagingIcon />
        </Link>
      </header>

      <Container as="main" className="flex-1 pt-6 pb-12 max-[640px]:px-3 max-[640px]:pt-4 max-[640px]:pb-8">
        {children}
      </Container>
      <Suspense fallback={null}>
        <CurrentSiteFooter />
      </Suspense>

      <nav
        aria-label="Main"
        className="fixed inset-x-0 bottom-0 z-[70] hidden border-t border-rs-nav-border bg-rs-surface font-rs-ui shadow-rs-bar-up max-[480px]:block"
      >
        <Tabs variant="phone" />
      </nav>
    </div>
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

const searchStyles = {
  // Up to 280px; between 641 and 760px only the icon shows until the field has the focus;
  // from 481 to 640px it fills the space beside the logo.
  top: {
    box: "h-[34px] flex-[0_1_280px] rounded-rs-xs px-2.5 max-[760px]:flex-[0_1_40px] max-[760px]:focus-within:flex-[0_1_280px] max-[640px]:flex-auto max-[640px]:focus-within:flex-auto",
    input: "max-[760px]:placeholder:text-transparent max-[640px]:placeholder:text-rs-nav-muted",
    placeholder: "Search researchers, projects, posts",
  },
  phone: { box: "h-9 flex-1 rounded-rs-pill px-3", input: "", placeholder: "Search researchers, projects" },
};

// SearchBox sends the query to the search page (FR-6), which a later slice builds.
function SearchBox({ id, variant }: { id: string; variant: keyof typeof searchStyles }) {
  const style = searchStyles[variant];
  return (
    <form
      action="/search"
      role="search"
      className={`flex min-w-0 items-center gap-2 bg-rs-search text-rs-body text-rs-nav-muted focus-within:shadow-focus ${style.box}`}
    >
      <label htmlFor={id} className="flex flex-none">
        <SearchIcon />
        <span className="sr-only">Search</span>
      </label>
      <input
        id={id}
        name="q"
        type="search"
        placeholder={style.placeholder}
        className={`min-w-0 flex-1 bg-transparent text-rs-nav-text outline-none placeholder:text-rs-nav-muted ${style.input}`}
      />
    </form>
  );
}
