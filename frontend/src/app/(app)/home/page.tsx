import { SiteFooter } from "@/components/site-footer";
import { AccessNotice } from "@/features/auth/access-notice";
import { Composer } from "@/features/home/composer";
import { EmptyFeed } from "@/features/home/empty-feed";
import { HomeProfileCard } from "@/features/profile/home-profile-card";
import {
  ProjectRecommendationList,
  ProjectRecommendationsProvider,
} from "@/features/project/project-recommendations";
import { RecommendationList } from "@/features/recommendation/recommendation-list";
import { RecommendationsProvider } from "@/features/recommendation/recommendations-provider";

// Home is the wrapping three-column row of design/screens/Home.html: the profile card, the
// feed and the recommendations with the compact site footer (the shell leaves its full
// footer out here; see footerFor). The columns share the row by their flex weights and wrap
// when it is too narrow; under 640px they stack at full width, and at 480px and narrower the
// profile card is left out, as the phone top bar carries the avatar (Home_m.html). The right
// column's recommended collaborators (AI-2) and "Projects for you" (AI-3.1) are each the top 3
// of the page's one request for that list. AccessNotice
// shows the message of a guard that sent the user here, such as RequireAdmin.
export default function Page() {
  return (
    <RecommendationsProvider>
      <ProjectRecommendationsProvider>
        <AccessNotice />
        <div className="flex flex-wrap items-start gap-6 max-[640px]:gap-4">
          <aside
            aria-label="Your profile"
            className="flex min-w-0 flex-[1_1_220px] flex-col gap-3 max-w-[260px] max-[640px]:max-w-none max-[480px]:hidden"
          >
            <HomeProfileCard />
          </aside>
          <section aria-label="Feed" className="flex min-w-0 flex-[999_1_480px] flex-col gap-3">
            <Composer />
            <EmptyFeed />
          </section>
          <aside
            aria-label="Recommendations"
            className="flex min-w-0 flex-[1_1_280px] flex-col gap-3 max-w-[320px] max-[640px]:max-w-none"
          >
            <RecommendationList limit={3} variant="home" />
            <ProjectRecommendationList limit={3} variant="home" />
            <SiteFooter variant="compact" />
          </aside>
        </div>
      </ProjectRecommendationsProvider>
    </RecommendationsProvider>
  );
}
