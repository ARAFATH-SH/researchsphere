import { WarningPage } from "@/features/moderation/warning-page";

// A warning from an administrator (FR-15.5); the account_warning notification links here.
export default async function Page({ params }: PageProps<"/warnings/[warningId]">) {
  const { warningId } = await params;
  return <WarningPage warningId={warningId} />;
}
