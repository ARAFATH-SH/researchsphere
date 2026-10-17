import { Suspense } from "react";

import { ProfileSkeleton } from "@/features/profile/profile-skeleton";
import { ProfileView } from "@/features/profile/profile-view";

// Any user's profile (FR-4.5). The signed-in user's own ID shows the own version, which
// reads ?edit=<section> and so needs Suspense.
export default async function Page({ params }: PageProps<"/profile/[userId]">) {
  const { userId } = await params;
  return (
    <Suspense fallback={<ProfileSkeleton />}>
      <ProfileView userId={userId} />
    </Suspense>
  );
}
