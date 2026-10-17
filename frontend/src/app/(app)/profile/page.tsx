import { Suspense } from "react";

import { ProfileSkeleton } from "@/features/profile/profile-skeleton";
import { ProfileView } from "@/features/profile/profile-view";

// The signed-in user's own profile (FR-4.5); the "Me" tab links here. ?edit=<section>
// opens that section's edit dialog (FR-4.1 to FR-4.4); reading it needs Suspense.
export default function Page() {
  return (
    <Suspense fallback={<ProfileSkeleton />}>
      <ProfileView />
    </Suspense>
  );
}
