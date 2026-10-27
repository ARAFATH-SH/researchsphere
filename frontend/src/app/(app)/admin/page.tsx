import { AdminConsole } from "@/features/auth/admin-console";
import { RequireAdmin } from "@/features/auth/require-admin";

// Administration (FR-15). Only an administrator sees it; anyone else goes to /home.
export default function Page() {
  return (
    <RequireAdmin>
      <AdminConsole />
    </RequireAdmin>
  );
}
