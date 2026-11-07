import { RequireAdmin } from "@/features/auth/require-admin";
import { UserModeration } from "@/features/moderation/user-moderation";

// A user's moderation history and actions (FR-15.9). Only an administrator sees it.
export default async function Page({ params }: PageProps<"/admin/users/[userId]">) {
  const { userId } = await params;
  return (
    <RequireAdmin>
      <UserModeration userId={userId} />
    </RequireAdmin>
  );
}
