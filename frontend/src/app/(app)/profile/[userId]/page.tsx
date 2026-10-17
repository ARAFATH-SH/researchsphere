import { ProfileView } from "@/features/profile/profile-view";

// Any user's profile (FR-4.5). The signed-in user's own ID shows the own version.
export default async function Page({ params }: PageProps<"/profile/[userId]">) {
  const { userId } = await params;
  return <ProfileView userId={userId} />;
}
