import { ApiStatus } from "@/features/health/api-status";

export function SiteFooter() {
  return (
    <footer className="mt-auto">
      <div className="mx-auto flex w-full max-w-page flex-wrap items-center justify-between gap-2 px-4 py-6 text-sm text-text-muted">
        <span>ResearchSphere</span>
        <ApiStatus />
      </div>
    </footer>
  );
}
