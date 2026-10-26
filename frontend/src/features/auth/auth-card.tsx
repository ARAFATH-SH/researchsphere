import Link from "next/link";
import type { ReactNode } from "react";

type AuthCardProps = {
  title: string;
  subtitle?: ReactNode;
  children?: ReactNode;
  // signInLink adds "Already on ResearchSphere? Sign in" under the content.
  signInLink?: boolean;
  // joinLink adds "New here? Join now" under the content.
  joinLink?: boolean;
};

// authLinkClass is a link in the text of an auth card: rs-accent-strong, 500, underlined.
export const authLinkClass =
  "font-medium text-rs-accent-strong underline hover:text-rs-accent-hover focus-visible:shadow-focus focus-visible:outline-none";

// AuthCard is the one card that signed-out forms sit in (design/screens/Auth.html): 363px
// wide (the board's column), full width under 640px, 12px radius, 24px padding, 14px between
// items. The title is the page's h1, styled as the design's card heading.
export function AuthCard({ title, subtitle, children, signInLink = false, joinLink = false }: AuthCardProps) {
  return (
    <section
      aria-labelledby="auth-card-title"
      className="mx-auto flex w-full max-w-[363px] flex-col gap-3.5 rounded-card-lg border border-rs-border bg-rs-surface p-6 shadow-card max-[640px]:max-w-none"
    >
      <h1 id="auth-card-title" className="text-rs-lead leading-rs-snug font-medium">
        {title}
      </h1>
      {subtitle ? <div className="text-rs-body leading-rs-relaxed">{subtitle}</div> : null}
      {children}
      {signInLink ? (
        <p className="text-center text-rs-text-muted">
          Already on ResearchSphere?{" "}
          <Link href="/login" className={authLinkClass}>
            Sign in
          </Link>
        </p>
      ) : null}
      {joinLink ? (
        <p className="text-center text-rs-text-muted">
          New here?{" "}
          <Link href="/register" className={authLinkClass}>
            Join now
          </Link>
        </p>
      ) : null}
    </section>
  );
}
