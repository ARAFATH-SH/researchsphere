import type { Metadata } from "next";

import { ChangePasswordForm } from "@/features/auth/change-password-form";
import { DeleteAccountForm } from "@/features/auth/delete-account-form";

export const metadata: Metadata = { title: "Account settings · ResearchSphere" };

// Account settings: one card per task, stacked in the signed-in shell's Container.
export default function Page() {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Account settings</h1>
      <ChangePasswordForm />
      <DeleteAccountForm />
    </div>
  );
}
