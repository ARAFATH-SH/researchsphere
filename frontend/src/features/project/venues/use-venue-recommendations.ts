"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { processingRetryMs } from "@/features/recommendation/recommendations-provider";
import type { Result } from "@/lib/api/call";

import { getRecommendedVenues, type RecommendedVenue, type VenueRecommendations } from "../api";

export type VenueRecsState =
  | { status: "loading" }
  // hidden: the API refused (not a member any more); the card shows nothing.
  | { status: "hidden" }
  | { status: "error"; notice: string | null }
  | { status: "needs_abstract"; notice: string }
  | { status: "processing"; notice: string }
  | { status: "ready"; venues: RecommendedVenue[]; journals: RecommendedVenue[]; notice: string };

// useVenueRecommendations reads a project's recommended venues (AI-6.1 to AI-6.4). It reads
// them again whenever abstractKey (the project's abstract) changes, so a saved abstract
// shows its venues, and while the answer is "processing" it asks once more after a minute.
export function useVenueRecommendations(projectId: string, abstractKey: string) {
  const [state, setState] = useState<VenueRecsState>({ status: "loading" });
  const generation = useRef(0);
  const retried = useRef(false);
  const notice = useRef<string | null>(null);

  const apply = useCallback((res: Result<VenueRecommendations>) => {
    if (!res.ok) {
      setState(
        res.code === "not_found" || res.code === "forbidden"
          ? { status: "hidden" }
          : { status: "error", notice: notice.current },
      );
      return;
    }
    const d = res.data;
    notice.current = d.notice;
    if (d.state === "needs_abstract") setState({ status: "needs_abstract", notice: d.notice });
    else if (d.state === "processing") setState({ status: "processing", notice: d.notice });
    else setState({ status: "ready", venues: d.venues, journals: d.journals, notice: d.notice });
  }, []);

  const load = useCallback(
    async (showLoading: boolean) => {
      const mine = ++generation.current;
      if (showLoading) setState({ status: "loading" });
      const res = await getRecommendedVenues(projectId);
      if (mine === generation.current) apply(res);
    },
    [projectId, apply],
  );

  // A new abstract asks again, and may again wait once for "processing".
  useEffect(() => {
    retried.current = false;
    const mine = ++generation.current;
    void getRecommendedVenues(projectId).then((res) => {
      if (mine === generation.current) apply(res);
    });
  }, [projectId, abstractKey, apply]);

  useEffect(() => {
    if (state.status !== "processing" || retried.current) return;
    const timer = setTimeout(() => {
      retried.current = true;
      void load(false);
    }, processingRetryMs);
    return () => clearTimeout(timer);
  }, [state.status, load]);

  const retry = useCallback(() => void load(true), [load]);
  return { state, retry };
}
