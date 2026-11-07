import { RequireAdmin } from "@/features/auth/require-admin";
import { ReportDetail } from "@/features/moderation/report-detail";

// One reported conversation (FR-16.9, NFR-14). Only an administrator sees it.
export default async function Page({ params }: PageProps<"/admin/reports/[reportId]">) {
  const { reportId } = await params;
  return (
    <RequireAdmin>
      <ReportDetail reportId={reportId} />
    </RequireAdmin>
  );
}
