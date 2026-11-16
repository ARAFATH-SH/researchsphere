import { ProjectPage } from "@/features/project/project-page";

// A project's page (FR-10, FR-15.11, AI-2.3).
export default async function Page({ params }: PageProps<"/projects/[projectId]">) {
  const { projectId } = await params;
  return <ProjectPage projectId={projectId} />;
}
