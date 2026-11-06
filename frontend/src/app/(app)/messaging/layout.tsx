import { MessagingShell } from "@/features/messaging/messaging-shell";

// The list stays mounted while the thread beside it changes.
export default function Layout({ children }: LayoutProps<"/messaging">) {
  return <MessagingShell>{children}</MessagingShell>;
}
