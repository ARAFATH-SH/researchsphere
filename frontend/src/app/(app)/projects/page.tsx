import { ComingSoon } from "@/components/coming-soon";

export default function Page() {
  return (
    <ComingSoon
      title="Projects"
      purpose="List your projects and create a project."
      requirements={["FR-10", "FR-11.6"]}
    />
  );
}
