import { WorkspacePage } from "@/features/project/workspace/workspace-page";

// A project's workspace (FR-12.1 to FR-12.3, FR-12.6): files and the LaTeX editor.
export default async function Page({ params }: PageProps<"/projects/[projectId]/workspace">) {
  const { projectId } = await params;
  return <WorkspacePage projectId={projectId} />;
}
