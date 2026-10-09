import type { Metadata } from "next";

import { AuthCard } from "@/features/auth/auth-card";

export const metadata: Metadata = { title: "Reset password · ResearchSphere" };

// Password reset is built in a later slice (FR-2.3 to FR-2.5).
export default function Page() {
  return (
    <AuthCard title="Reset password" subtitle="Ask for a reset link and choose a new password." signInLink>
      <p className="inline-block rounded-full bg-primary-soft px-3 py-1 text-sm font-semibold text-primary">Coming soon</p>
      <p className="text-sm text-text-muted">Built in a later slice: FR-2.</p>
    </AuthCard>
  );
}
