import Link from "next/link";
import { type ReactNode, Suspense } from "react";

import { Container } from "@/components/Container";
import { Logo } from "@/components/logo";
import { CurrentSiteFooter } from "@/components/site-footer";

import { MessagingIcon } from "./icons";
import { MeMenu } from "./me-menu";
import { SearchBox, type SearchBoxVariant, StaticSearchBox } from "./search-box";
import { CurrentTabList, TabList, type TabListVariant } from "./tab-list";

// AppShell is the signed-in frame (docs/ui-guidelines.md, Signed-in pages): a sticky 52px
// top bar with the logo, search and icon tabs, whose tabs move to a 56px bottom bar from 481
// to 640px. At 480px and narrower a phone top bar (avatar, search, messages) and a 64px
// bottom bar replace it. The full site footer follows the page where footerFor says so.
export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className="flex flex-1 flex-col max-[640px]:pb-14 max-[480px]:pt-14 max-[480px]:pb-16">
      <header className="sticky top-0 z-50 h-[52px] border-b border-rs-nav-border bg-rs-surface font-rs-ui text-rs-nav-text max-[480px]:hidden">
        <Container className="flex h-[52px] items-center gap-2">
          <Logo href="/home" markOnly />
          <Search id="site-search" variant="top" />
          <nav aria-label="Main" className="ml-auto flex h-full">
            <Tabs variant="top" />
          </nav>
        </Container>
      </header>

      <header className="fixed inset-x-0 top-0 z-[70] hidden h-14 items-center gap-3 border-b border-rs-nav-border bg-rs-surface px-3 font-rs-ui text-rs-nav-text shadow-rs-bar max-[480px]:flex">
        <MeMenu placement="phone" />
        <Search id="phone-search" variant="phone" />
        <Link
          href="/messaging"
          aria-label="Messaging"
          className="relative flex size-11 flex-none items-center justify-center rounded-full text-rs-nav-muted focus-visible:shadow-focus focus-visible:outline-none"
        >
          <MessagingIcon />
        </Link>
      </header>

      <Container as="main" className="flex-1 pt-6 pb-12 max-[640px]:pt-4 max-[640px]:pb-8">
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

// Search reads the URL, so it prerenders as a plain box and then shows the current query.
function Search({ id, variant }: { id: string; variant: SearchBoxVariant }) {
  return (
    <Suspense fallback={<StaticSearchBox id={id} variant={variant} />}>
      <SearchBox id={id} variant={variant} />
    </Suspense>
  );
}
