import { PlaceholderPage } from "@/components/placeholder-page";

export default function Page() {
  return (
    <PlaceholderPage
      title="Search"
      purpose="Search researchers, projects and posts by keyword or meaning."
      requirements={["FR-6", "FR-10.3", "AI-5"]}
    />
  );
}
