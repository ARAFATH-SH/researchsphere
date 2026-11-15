"use client";

import Link from "next/link";

import { SiteFooter } from "@/components/site-footer";
import { homeCard } from "@/features/home/cards";

import { maxRecommendations } from "./api";
import { RecommendationList } from "./recommendation-list";
import { RecommendationsProvider } from "./recommendations-provider";

// RecommendationsPage is /network/recommendations: all of the caller's collaborator
// recommendations (AI-2.1, up to 10) in the My Network layout, a strip with the title and
// the way back, then the list beside the compact footer.
export function RecommendationsPage() {
  return (
    <RecommendationsProvider>
      <div className="flex flex-col gap-5 max-[640px]:gap-4">
        <section
          aria-label="Recommendations summary"
          className={`${homeCard} flex flex-wrap items-center gap-3 px-[18px] py-3.5`}
        >
          <h1 className="flex-[1_1_220px] text-rs-h3 leading-rs-tight font-bold text-rs-text">
            Recommended collaborators
          </h1>
          <Link href="/network" className="text-rs-body font-medium text-rs-accent-strong hover:underline">
            Back to My Network
          </Link>
        </section>
        <div className="flex flex-wrap items-start gap-5 max-[640px]:gap-4">
          <div className="flex min-w-0 flex-[999_1_480px] flex-col gap-4">
            <RecommendationList limit={maxRecommendations} variant="page" />
          </div>
          <aside
            aria-label="More"
            className="flex max-w-[320px] min-w-0 flex-[1_1_280px] flex-col gap-4 max-[640px]:max-w-none"
          >
            <SiteFooter variant="compact" />
          </aside>
        </div>
      </div>
    </RecommendationsProvider>
  );
}
