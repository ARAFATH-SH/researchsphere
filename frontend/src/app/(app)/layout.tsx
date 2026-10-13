import { AppShell } from "@/components/app-shell/app-shell";
import { RequireSession } from "@/features/auth/require-session";

// Signed-in pages inside the shell (docs/ui-guidelines.md). Their content shows only
// to a signed-in user; anyone else goes to /login.
export default function SignedInLayout({ children }: LayoutProps<"/">) {
  return (
    <AppShell>
      <RequireSession>{children}</RequireSession>
    </AppShell>
  );
}
