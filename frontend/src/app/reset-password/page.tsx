import { PlaceholderPage } from "@/components/placeholder-page";

export default function Page() {
  return (
    <PlaceholderPage
      title="Reset password"
      purpose="Ask for a reset link and choose a new password."
      requirements={["FR-2"]}
    />
  );
}
