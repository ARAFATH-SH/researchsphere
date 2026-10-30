import { PublicationListPage } from "@/features/profile/publication-list-page";

// All of a user's publications, paged (FR-5.5); "Show all N publications" on the profile
// links here.
export default async function Page({
  params,
}: PageProps<"/profile/[userId]/publications">) {
  const { userId } = await params;
  return <PublicationListPage userId={userId} />;
}
