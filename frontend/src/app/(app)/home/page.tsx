import { ComingSoon } from "@/components/coming-soon";
import { Card } from "@/components/ui/card";

// Home reserves the three-column grid of docs/ui-guidelines.md: profile card, feed,
// recommendations. Each column is filled by a later slice.
export default function Page() {
  return (
    <div className="grid gap-6 md:grid-cols-[225px_minmax(0,1fr)] lg:grid-cols-[225px_minmax(0,1fr)_300px]">
      <aside aria-label="Your profile">
        <Card className="h-40 p-4 text-sm text-text-muted">Your profile card (FR-4)</Card>
      </aside>
      <section aria-label="Feed">
        <ComingSoon
          title="Home"
          purpose="Read the feed and see recommended collaborators and projects."
          requirements={["FR-8", "AI-2", "AI-3"]}
        />
      </section>
      <aside aria-label="Recommendations" className="md:col-start-2 lg:col-start-auto">
        <Card className="h-56 p-4 text-sm text-text-muted">Recommended collaborators and projects (AI-2, AI-3)</Card>
      </aside>
    </div>
  );
}
