import { PlaceholderPage } from "@/components/placeholder-page";

export default function Page() {
  return (
    <PlaceholderPage
      title="Home"
      purpose="Read the feed and see recommended collaborators and projects."
      requirements={["FR-8", "AI-2", "AI-3"]}
    />
  );
}
