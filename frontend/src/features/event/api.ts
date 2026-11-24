import { call, type Result, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";

export type Venue = components["schemas"]["Venue"];
export type VenueSummary = components["schemas"]["VenueSummary"];
export type VenuePage = components["schemas"]["VenuePage"];
export type VenueFacets = components["schemas"]["VenueFacets"];
export type VenueTime = components["schemas"]["VenueTime"];
export type VenueType = components["schemas"]["VenueType"];
export type VenueAttendance = components["schemas"]["VenueAttendance"];
export type VenueSource = VenueSummary["source"];
export type ResearchField = components["schemas"]["ResearchField"];
export type VenueInput = components["schemas"]["VenueInput"];
export type VenuePatch = components["schemas"]["VenuePatch"];
export type VenueImportResult = components["schemas"]["VenueImportResult"];
export type VenueImportRow = components["schemas"]["VenueImportRow"];
export type VenueImportReason = components["schemas"]["VenueImportReason"];
export type VenueSyncStatus = components["schemas"]["VenueSyncStatus"];
export type VenueSyncSource = components["schemas"]["VenueSyncSource"];
export type VenueSyncRun = components["schemas"]["VenueSyncRun"];
export type VenueSyncSourceName = components["schemas"]["VenueSyncSourceName"];
export type VenueSyncStarted = components["schemas"]["VenueSyncStarted"];

// VenueQuery is one request of the venue list (FR-14.1 to FR-14.3), as the API takes it.
export type VenueQuery = {
  page: number;
  pageSize: number;
  rolling?: boolean;
  includePast?: boolean;
  types?: string[];
  fields?: string[];
  deadlineFrom?: string;
  deadlineTo?: string;
  tz?: string;
  q?: string;
};

function withData<T>(res: Result<T | undefined>): Result<T> {
  if (!res.ok) return res;
  return res.data ? { ok: true, data: res.data } : { ok: false, ...unexpectedError };
}

// listVenues returns one page of venues with the facet counts. Empty values are left out.
export async function listVenues(q: VenueQuery): Promise<Result<VenuePage>> {
  const query = {
    page: q.page,
    pageSize: q.pageSize,
    ...(q.rolling ? { rolling: true } : {}),
    ...(q.includePast ? { includePast: true } : {}),
    ...(q.types?.length ? { type: q.types } : {}),
    ...(q.fields?.length ? { field: q.fields } : {}),
    ...(q.deadlineFrom ? { deadlineFrom: q.deadlineFrom } : {}),
    ...(q.deadlineTo ? { deadlineTo: q.deadlineTo } : {}),
    ...((q.deadlineFrom || q.deadlineTo) && q.tz ? { tz: q.tz } : {}),
    ...(q.q ? { q: q.q } : {}),
  };
  return withData(await call(() => api.GET("/venues", { params: { query } })));
}

// listResearchFields returns the fixed list of research fields, in the order to show them.
export async function listResearchFields(): Promise<Result<ResearchField[]>> {
  const res = withData(await call(() => api.GET("/venues/fields")));
  return res.ok ? { ok: true, data: res.data.items } : res;
}

// getVenue returns a venue with its scope; a removed or unknown one is not_found.
export async function getVenue(venueId: string): Promise<Result<Venue>> {
  return withData(await call(() => api.GET("/venues/{venueId}", { params: { path: { venueId } } })));
}

// createVenue adds a venue (FR-15.1). A 400 names the first field at fault.
export async function createVenue(body: VenueInput): Promise<Result<Venue>> {
  return withData(await call(() => api.POST("/admin/venues", { body })));
}

// updateVenue changes the given fields; null clears an optional one.
export async function updateVenue(venueId: string, body: VenuePatch): Promise<Result<Venue>> {
  return withData(await call(() => api.PATCH("/admin/venues/{venueId}", { params: { path: { venueId } }, body })));
}

// removeVenue hides a venue for good, with the reason kept in the audit trail.
export async function removeVenue(venueId: string, reason: string): Promise<Result<undefined>> {
  const res = await call(() =>
    api.POST("/admin/venues/{venueId}/remove", { params: { path: { venueId } }, body: { reason } }),
  );
  return res.ok ? { ok: true, data: undefined } : res;
}

// importVenues sends a CSV or JSON file; dryRun reports what would happen and changes nothing.
export async function importVenues(file: File, dryRun: boolean): Promise<Result<VenueImportResult>> {
  return withData(
    await call(() =>
      api.POST("/admin/venues/import", {
        params: { query: { dryRun } },
        // openapi-typescript types a binary part as string; the serializer sends the File.
        body: { file: file as unknown as string },
        bodySerializer: () => {
          const form = new FormData();
          form.append("file", file);
          return form;
        },
      }),
    ),
  );
}

// getVenueSyncStatus returns every source of the automatic import with its latest runs.
export async function getVenueSyncStatus(): Promise<Result<VenueSyncStatus>> {
  return withData(await call(() => api.GET("/admin/venues/sync")));
}

// startVenueSync starts a run of the named sources (every source when empty).
export async function startVenueSync(sources: VenueSyncSourceName[], dryRun: boolean): Promise<Result<VenueSyncStarted>> {
  return withData(
    await call(() =>
      api.POST("/admin/venues/sync", { params: { query: { dryRun } }, body: sources.length ? { sources } : {} }),
    ),
  );
}
