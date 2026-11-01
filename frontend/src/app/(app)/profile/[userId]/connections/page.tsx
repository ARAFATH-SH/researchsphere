import { ProfileConnectionsPage } from "@/features/social/profile-connections-page";

// Any user's connections, paged (FR-7.4); "N connections" on their profile links here.
export default async function Page({
  params,
}: PageProps<"/profile/[userId]/connections">) {
  const { userId } = await params;
  return <ProfileConnectionsPage userId={userId} />;
}
