import type { Metadata } from "next";

import { ChangePasswordForm } from "@/features/auth/change-password-form";

export const metadata: Metadata = { title: "Account settings · ResearchSphere" };

// Account settings: one card per task, stacked in the signed-in shell's Container.
export default function Page() {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Account settings</h1>
      <ChangePasswordForm />
      {/* The "Delete account" card (FR-2.8, FR-2.9) goes here. */}
    </div>
  );
}
