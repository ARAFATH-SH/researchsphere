import { Suspense } from "react";

import { ProfileSkeleton } from "@/features/profile/profile-skeleton";
import { ProfileView } from "@/features/profile/profile-view";
import { RecommendationsProvider } from "@/features/recommendation/recommendations-provider";

// The signed-in user's own profile (FR-4.5); the "Me" tab links here. ?edit=<section>
// opens that section's edit dialog (FR-4.1 to FR-4.4); reading it needs Suspense. The
// sidebar's "Connect with researchers" reads the page's one recommendations request.
export default function Page() {
  return (
    <RecommendationsProvider>
      <Suspense fallback={<ProfileSkeleton />}>
        <ProfileView />
      </Suspense>
    </RecommendationsProvider>
  );
}
