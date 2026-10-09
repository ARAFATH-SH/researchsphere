import { ComingSoon } from "@/components/coming-soon";

export default function Page() {
  return (
    <ComingSoon
      title="Profile"
      purpose="View a profile, or edit your own, including publications."
      requirements={["FR-4", "FR-5"]}
    />
  );
}
