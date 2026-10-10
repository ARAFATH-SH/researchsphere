import type { Metadata } from "next";

import { AuthCard } from "@/features/auth/auth-card";

export const metadata: Metadata = { title: "Sign in · ResearchSphere" };

// Sign-in is built in slice 8 (FR-2).
export default function Page() {
  return (
    <AuthCard title="Sign in" subtitle="Welcome back to ResearchSphere.">
      <p className="inline-block rounded-full bg-primary-soft px-3 py-1 text-sm font-semibold text-primary">Coming soon</p>
      <p className="text-sm text-text-muted">Signing in is built in a later slice: FR-2.</p>
    </AuthCard>
  );
}
