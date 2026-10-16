import { AdminUsers } from "@/features/auth/admin-users";
import { RequireAdmin } from "@/features/auth/require-admin";

// Administration (FR-15). Only an administrator sees it; anyone else goes to /home.
export default function Page() {
  return (
    <RequireAdmin>
      <AdminUsers />
    </RequireAdmin>
  );
}
