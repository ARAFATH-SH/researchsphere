import type { Metadata } from "next";

import { VerifyEmail } from "@/features/auth/verify-email";

export const metadata: Metadata = {
  title: "Verify your email · ResearchSphere",
  // The page is reached from an emailed link; do not pass its address on to other sites.
  referrer: "no-referrer",
};

export default function Page() {
  return <VerifyEmail />;
}
