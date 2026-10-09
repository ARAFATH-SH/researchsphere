import { ComingSoon } from "@/components/coming-soon";

export default function Page() {
  return (
    <ComingSoon
      title="Search"
      purpose="Search researchers, projects and posts by keyword or meaning."
      requirements={["FR-6", "FR-10.3", "AI-5"]}
    />
  );
}
