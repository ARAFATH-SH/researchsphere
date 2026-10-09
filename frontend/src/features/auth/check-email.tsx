"use client";

import { AuthCard } from "./auth-card";
import { usePendingEmail } from "./pending-email";
import { ResendForm } from "./resend-form";

// CheckEmail tells a new user to open the link we emailed (FR-1.3) and offers a new one
// (FR-1.5).
export function CheckEmail() {
  const email = usePendingEmail();
  return (
    <AuthCard
      title="Check your email"
      subtitle={
        <>
          We sent a verification link to {email ? <strong className="text-text">{email}</strong> : "your email address"}.
          Open it to activate your account. The link works once and expires after 24 hours.
        </>
      }
      signInLink
    >
      <div className="space-y-3 border-t border-border pt-5">
        <h2 className="font-semibold">No email?</h2>
        <p className="text-sm text-text-muted">Check your spam folder, or ask for a new link. A new link replaces the old one.</p>
        <ResendForm />
      </div>
    </AuthCard>
  );
}
