import type { Metadata } from "next";

import { ResetPassword } from "@/features/auth/reset-password";

export const metadata: Metadata = {
  title: "Reset password · ResearchSphere",
  // The page is reached from an emailed link; do not pass its address on to other sites.
  referrer: "no-referrer",
};

export default function Page() {
  return <ResetPassword />;
}
