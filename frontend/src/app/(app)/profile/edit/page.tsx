import { redirect } from "next/navigation";

// Profile editing happens in dialogs on the profile itself (FR-4.1 to FR-4.4), so the
// old edit address goes there.
export default function Page() {
  redirect("/profile");
}
