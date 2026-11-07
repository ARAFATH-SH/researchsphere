import { SectionLayout, type SectionLink } from "@/components/section-layout";

import { AdminReports } from "@/features/moderation/admin-reports";

import { AdminUsers } from "./admin-users";

// Only the sections that are built, in the design's order. System health, Venues and Content
// moderation come with later slices.
const sections: SectionLink[] = [
  { id: "reported-messages", label: "Reported messages" },
  { id: "users", label: "Users" },
];

// AdminConsole is the administration page (design/screens/Admin.html). RequireAdmin guards it.
export function AdminConsole() {
  return (
    <>
      <h1 className="sr-only">Admin console</h1>
      <SectionLayout title="Admin console" navLabel="Admin sections" sections={sections} variant="admin">
        <AdminReports />
        <AdminUsers />
      </SectionLayout>
    </>
  );
}
