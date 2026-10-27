import Link from "next/link";

import { EventsIcon } from "@/components/app-shell/icons";
import { Container } from "@/components/Container";
import { Logo } from "@/components/logo";

const pill =
  "flex h-9 items-center rounded-rs-pill px-[18px] text-rs-body max-[640px]:px-4 font-medium whitespace-nowrap focus-visible:shadow-focus focus-visible:outline-none";

// PublicHeader is the signed-out top bar (docs/ui-guidelines.md, Signed-out pages): the
// logo and name, then Events (hidden under 640px), "Sign in" and "Join now". The pills
// are 16px narrower each side under 640px so they stay on one line at 390px.
export function PublicHeader() {
  return (
    <header className="sticky top-0 z-50 h-[52px] border-b border-rs-nav-border bg-rs-surface font-rs-ui text-rs-nav-text">
      <Container className="flex h-[52px] items-center gap-2">
        <Logo />
        <nav aria-label="Visitor" className="ml-auto flex items-center gap-2">
          <Link
            href="/events"
            className="flex h-[52px] flex-col items-center justify-center gap-0.5 px-3 text-rs-tiny leading-rs-tight tracking-[.01em] text-rs-nav-muted hover:text-rs-accent focus-visible:text-rs-accent focus-visible:shadow-focus focus-visible:outline-none max-[640px]:hidden"
          >
            <EventsIcon />
            Events
          </Link>
          <Link href="/login" className={`${pill} border border-rs-accent text-rs-accent hover:bg-rs-accent-soft`}>
            Sign in
          </Link>
          <Link href="/register" className={`${pill} bg-rs-accent text-rs-surface hover:bg-rs-accent-hover`}>
            Join now
          </Link>
        </nav>
      </Container>
    </header>
  );
}
