import { PlaceholderPage } from "@/components/placeholder-page";

export default function Page() {
  return (
    <PlaceholderPage
      title="Project"
      purpose="See details and members, handle requests, and see recommended researchers and venues."
      requirements={["FR-10", "FR-11", "AI-2.3", "AI-6"]}
    />
  );
}
