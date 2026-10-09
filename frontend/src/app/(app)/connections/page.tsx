import { ComingSoon } from "@/components/coming-soon";

export default function Page() {
  return (
    <ComingSoon
      title="My network"
      purpose="Handle connection requests and see your connections."
      requirements={["FR-7"]}
    />
  );
}
