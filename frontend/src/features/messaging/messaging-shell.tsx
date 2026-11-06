"use client";

import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

import { homeCard } from "@/features/home/cards";

import { ConversationList } from "./conversation-list";
import { ConversationsProvider } from "./use-conversations";

// Heights under the signed-in shell, so the card fills the screen and its columns scroll on
// their own: the top bar and main's padding, plus the bottom bar under 640px.
const height = "h-[calc(100dvh-124px)] max-[640px]:h-[calc(100dvh-156px)] max-[480px]:h-[calc(100dvh-168px)] min-h-[420px]";

// MessagingShell is the card of design/screens/Messaging.html. From 641px the list and the
// thread sit side by side. Under that they are separate screens: /messaging is the list and
// anything below it the thread, which has a back button.
export function MessagingShell({ children }: { children: ReactNode }) {
  const pathname = usePathname() ?? "";
  const inThread = pathname.startsWith("/messaging/");
  return (
    <ConversationsProvider>
      <div className={`${homeCard} ${height} flex overflow-hidden`}>
        <section
          aria-label="Conversations"
          className={`flex w-[320px] min-w-0 shrink-0 flex-col border-r border-rs-border max-[640px]:w-full max-[640px]:border-r-0 ${
            inThread ? "max-[640px]:hidden" : ""
          }`}
        >
          <ConversationList />
        </section>
        <div className={`min-w-0 flex-1 ${inThread ? "" : "max-[640px]:hidden"}`}>{children}</div>
      </div>
    </ConversationsProvider>
  );
}
