import { SectionLayout, type SectionLink } from "@/components/section-layout";

import { AdminUsers } from "./admin-users";

// Only Users is built. System health, Venues, Content moderation and Reported messages from
// the design come with later slices.
const sections: SectionLink[] = [{ id: "users", label: "Users" }];

// AdminConsole is the administration page (design/screens/Admin.html). RequireAdmin guards it.
export function AdminConsole() {
  return (
    <>
      <h1 className="sr-only">Admin console</h1>
      <SectionLayout title="Admin console" navLabel="Admin sections" sections={sections} variant="admin">
        <AdminUsers />
      </SectionLayout>
    </>
  );
}
