import { Suspense } from "react";

import { SearchPage } from "@/features/search/search-page";
import { SearchSkeleton } from "@/features/search/search-results";

// Search for researchers (FR-6.1 to FR-6.3) and open projects (FR-10.3). Posts come with their slice.
export default function Page() {
  return (
    <Suspense fallback={<SearchSkeleton />}>
      <SearchPage />
    </Suspense>
  );
}
