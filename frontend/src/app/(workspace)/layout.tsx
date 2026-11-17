import { RequireSession } from "@/features/auth/require-session";
import { RealtimeConnector } from "@/features/realtime/realtime-connector";

// The project workspace fills the screen with its own top bar (design/screens/Workspace.html),
// so it sits outside the signed-in shell of (app) (docs/decisions.md, 2026-11-17, slice 21.2).
// It is still for signed-in users only, and holds the socket as the shell does.
export default function WorkspaceLayout({ children }: LayoutProps<"/">) {
  return (
    <div className="flex flex-1 flex-col">
      <RealtimeConnector />
      <RequireSession>{children}</RequireSession>
    </div>
  );
}
