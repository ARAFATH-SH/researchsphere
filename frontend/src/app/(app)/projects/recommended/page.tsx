import { ProjectRecommendationList, ProjectRecommendationsProvider } from "@/features/project/project-recommendations";

// All of the caller's project recommendations (AI-3.1), linked from Home.
export default function Page() {
  return (
    <ProjectRecommendationsProvider>
      <ProjectRecommendationList limit={10} variant="page" />
    </ProjectRecommendationsProvider>
  );
}
