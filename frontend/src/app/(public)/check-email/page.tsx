import type { Metadata } from "next";

import { CheckEmail } from "@/features/auth/check-email";

export const metadata: Metadata = { title: "Check your email · ResearchSphere" };

export default function Page() {
  return <CheckEmail />;
}
