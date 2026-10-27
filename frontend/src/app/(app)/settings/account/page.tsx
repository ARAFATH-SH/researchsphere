import type { Metadata } from "next";

import { AccountSettings } from "@/features/auth/account-settings";

export const metadata: Metadata = { title: "Account settings · ResearchSphere" };

// Account settings: the section nav and one card per task (design/screens/Settings.html).
export default function Page() {
  return <AccountSettings />;
}
