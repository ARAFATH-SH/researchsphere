import { VenuePage } from "@/features/event/venue-page";

// A venue's own page with its full scope (FR-14.4).
export default async function Page({ params }: PageProps<"/events/[venueId]">) {
  const { venueId } = await params;
  return <VenuePage venueId={venueId} />;
}
