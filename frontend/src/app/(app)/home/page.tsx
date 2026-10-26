import { ComingSoon } from "@/components/coming-soon";
import { SiteFooter } from "@/components/site-footer";
import { Card } from "@/components/ui/card";
import { AccessNotice } from "@/features/auth/access-notice";
import { HomeProfileCard } from "@/features/profile/home-profile-card";

// Home is the three-column grid of docs/ui-guidelines.md: the profile card, then the feed
// and recommendations, which later slices fill. The compact site footer ends the right column
// (the shell leaves its full footer out here; see footerFor). AccessNotice shows the message of a guard
// that sent the user here, such as RequireAdmin.
export default function Page() {
  return (
    <>
      <AccessNotice />
      <div className="grid gap-6 md:grid-cols-[240px_minmax(0,1fr)] lg:grid-cols-[240px_minmax(0,1fr)_320px]">
        <aside aria-label="Your profile">
          <HomeProfileCard />
        </aside>
        <section aria-label="Feed">
          <ComingSoon
            title="Home"
            purpose="Read the feed and see recommended collaborators and projects."
            requirements={["FR-8", "AI-2", "AI-3"]}
          />
        </section>
        <div className="flex flex-col gap-3 md:col-start-2 lg:col-start-auto">
          <aside aria-label="Recommendations">
            <Card className="h-56 p-4 text-sm text-text-muted">Recommended collaborators and projects (AI-2, AI-3)</Card>
          </aside>
          <SiteFooter variant="compact" />
        </div>
      </div>
    </>
  );
}
