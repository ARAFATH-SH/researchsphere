import Link from "next/link";

import { Logo } from "@/components/logo";
import { buttonClass } from "@/components/ui/button";

// PublicHeader is the signed-out top bar: logo left, "Join now" and "Sign in" right.
export function PublicHeader() {
  return (
    <header>
      <div className="mx-auto flex w-full max-w-page items-center justify-between gap-4 px-4 py-4">
        <Logo compactBelow="sm" />
        <nav aria-label="Account" className="flex items-center gap-1 sm:gap-2">
          <Link href="/register" className={buttonClass({ variant: "text", size: "md" })}>
            Join now
          </Link>
          <Link href="/login" className={buttonClass({ variant: "outline", size: "md" })}>
            Sign in
          </Link>
        </nav>
      </div>
    </header>
  );
}
