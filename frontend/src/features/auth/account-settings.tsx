import { SectionLayout, type SectionLink } from "@/components/section-layout";
import { BlockedPeople } from "@/features/social/blocked-people";

import { ChangePasswordForm } from "./change-password-form";
import { DeleteAccountForm } from "./delete-account-form";

// Only the sections that are built: the design's Devices & sessions needs a list of sessions
// the API does not have yet, and AI assistant comes with a later slice.
const sections: SectionLink[] = [
  { id: "change-password", label: "Sign-in & security" },
  { id: "messaging-email", label: "Messaging & email" },
  { id: "delete-account", label: "Close account" },
];

// AccountSettings is the settings page (design/screens/Settings.html): the section nav and
// one card per task.
export function AccountSettings() {
  return (
    <>
      <h1 className="sr-only">Account settings</h1>
      <SectionLayout title="Settings" navLabel="Settings" sections={sections}>
        <ChangePasswordForm />
        <BlockedPeople />
        <DeleteAccountForm />
      </SectionLayout>
    </>
  );
}
