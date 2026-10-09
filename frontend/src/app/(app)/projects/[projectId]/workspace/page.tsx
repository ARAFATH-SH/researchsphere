import { ComingSoon } from "@/components/coming-soon";

export default function Page() {
  return (
    <ComingSoon
      title="Workspace"
      purpose="Edit files, compile, read the PDF and use the assistant."
      requirements={["FR-12", "FR-13", "AI-7", "AI-8"]}
      note="Real-time editing comes in slice 22 and compiling in slice 23. The workspace needs a screen at least 1280 pixels wide."
    />
  );
}
