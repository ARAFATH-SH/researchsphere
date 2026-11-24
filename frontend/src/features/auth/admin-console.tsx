import { SectionLayout, type SectionLink } from "@/components/section-layout";

import { AdminVenues } from "@/features/event/admin/admin-venues";
import { VenueSync } from "@/features/event/admin/venue-sync";
import { AdminReports } from "@/features/moderation/admin-reports";

import { AdminUsers } from "./admin-users";

// Only the sections that are built, in the design's order. System health and Content
// moderation come with later slices. The automatic venue import sits under Venues.
const sections: SectionLink[] = [
  { id: "venues", label: "Venues" },
  { id: "reported-messages", label: "Reported messages" },
  { id: "users", label: "Users" },
];

// AdminConsole is the administration page (design/screens/Admin.html). RequireAdmin guards it.
export function AdminConsole() {
  return (
    <>
      <h1 className="sr-only">Admin console</h1>
      <SectionLayout title="Admin console" navLabel="Admin sections" sections={sections} variant="admin">
        <AdminVenues />
        <VenueSync />
        <AdminReports />
        <AdminUsers />
      </SectionLayout>
    </>
  );
}
