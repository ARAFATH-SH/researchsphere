"use client";

import { AuthCard } from "./auth-card";
import { usePendingEmail } from "./pending-email";
import { ResendForm } from "./resend-form";

// CheckEmail tells a new user to open the link we emailed (FR-1.3) and offers a new one
// (FR-1.5). Laid out as the "Check your email" card in design/screens/Auth.html.
export function CheckEmail() {
  const email = usePendingEmail();
  return (
    <AuthCard title="Check your email" signInLink>
      <span className="flex size-14 items-center justify-center rounded-full bg-rs-accent-soft text-rs-accent-strong">
        <svg
          width="28"
          height="28"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <rect x="3" y="5" width="18" height="14" rx="2" />
          <path d="M3 7l9 6 9-6" />
        </svg>
      </span>
      <p className="leading-rs-relaxed">
        We sent a verification link to {email ? <strong>{email}</strong> : "your email address"}. Open it to activate
        your account. The link works once and expires after 24 hours.
      </p>
      <p className="text-rs-text-muted">No email? Check your spam folder, or ask for a new link. A new link replaces the old one.</p>
      <ResendForm />
    </AuthCard>
  );
}
