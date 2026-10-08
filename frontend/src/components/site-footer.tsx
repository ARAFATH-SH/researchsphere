import { ApiStatus } from "@/features/health/api-status";

export function SiteFooter() {
  return (
    <footer className="border-t border-gray-200 dark:border-gray-800">
      <div className="mx-auto flex w-full max-w-5xl flex-wrap items-center justify-between gap-2 px-4 py-3 text-sm text-gray-600 dark:text-gray-400">
        <span>ResearchSphere</span>
        <ApiStatus />
      </div>
    </footer>
  );
}
