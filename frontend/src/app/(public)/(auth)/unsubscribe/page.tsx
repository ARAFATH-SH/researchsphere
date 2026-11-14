import type { Metadata } from "next";

import { Unsubscribe } from "@/features/messaging/unsubscribe";

export const metadata: Metadata = {
  title: "Unread message emails · ResearchSphere",
  // The page is reached from an emailed link; do not pass its address on to other sites.
  referrer: "no-referrer",
};

export default function Page() {
  return <Unsubscribe />;
}
