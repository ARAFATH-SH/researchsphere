import { AppShell } from "@/components/app-shell/app-shell";

// Signed-in pages inside the shell (docs/ui-guidelines.md). Nothing checks the session
// yet: login is built in slice 8, which adds that check here.
export default function SignedInLayout({ children }: LayoutProps<"/">) {
  return <AppShell>{children}</AppShell>;
}
