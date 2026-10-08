import { PlaceholderPage } from "@/components/placeholder-page";

export default function Page() {
  return (
    <PlaceholderPage
      title="Welcome to ResearchSphere"
      purpose="Find collaborators, write papers together and choose where to publish."
      requirements={["FR-1", "FR-2"]}
    />
  );
}
