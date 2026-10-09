import { ComingSoon } from "@/components/coming-soon";

export default function Page() {
  return (
    <ComingSoon
      title="Admin"
      purpose="Maintain venues, remove content and manage users."
      requirements={["FR-15"]}
    />
  );
}
