import { networkError, unexpectedError } from "@/lib/api/call";
import { api, type components } from "@/lib/api/client";

export type Publication = components["schemas"]["Publication"];
export type PublicationAdded = components["schemas"]["PublicationAdded"];
export type PublicationPage = components["schemas"]["PublicationPage"];
export type PublicationPreview = components["schemas"]["PublicationPreview"];
export type PublicationCreate = components["schemas"]["PublicationCreate"];
export type PublicationUpdate = components["schemas"]["PublicationUpdate"];
export type OrcidWork = components["schemas"]["OrcidWork"];
export type OrcidWorkList = components["schemas"]["OrcidWorkList"];
export type OrcidImportResult = components["schemas"]["OrcidImportResult"];

// PubFailure is a refused publication request. retryAfter is the Retry-After header in
// seconds, sent with rate_limited; call() in lib/api drops headers, hence this helper.
export type PubFailure = {
  ok: false;
  status: number;
  code: string;
  message: string;
  retryAfter?: number;
};

export type PubResult<T> = { ok: true; data: T } | PubFailure;

async function request<T>(
  send: () => Promise<{
    data?: T;
    error?: { code?: string; message?: string };
    response: Response;
  }>,
  empty?: T,
): Promise<PubResult<T>> {
  try {
    const { data, error, response } = await send();
    if (response.ok) {
      const value = data ?? empty;
      return value === undefined
        ? { ok: false, status: response.status, ...unexpectedError }
        : { ok: true, data: value };
    }
    const retry = Number.parseInt(
      response.headers.get("Retry-After") ?? "",
      10,
    );
    const { code, message } =
      error?.code && error.message
        ? { code: error.code, message: error.message }
        : unexpectedError;
    return {
      ok: false,
      status: response.status,
      code,
      message,
      retryAfter: Number.isFinite(retry) ? retry : undefined,
    };
  } catch {
    return { ok: false, status: 0, ...networkError };
  }
}

// listMyPublications returns one page of the signed-in user's confirmed publications (FR-5.5).
export function listMyPublications(
  page: number,
  pageSize: number,
): Promise<PubResult<PublicationPage>> {
  return request(() =>
    api.GET("/profiles/me/publications", {
      params: { query: { page, pageSize } },
    }),
  );
}

// listPublications returns one page of any user's confirmed publications (FR-5.5). An
// unknown, suspended or unverified account is 404.
export function listPublications(
  userId: string,
  page: number,
  pageSize: number,
): Promise<PubResult<PublicationPage>> {
  return request(() =>
    api.GET("/profiles/{userId}/publications", {
      params: { path: { userId }, query: { page, pageSize } },
    }),
  );
}

// lookupDOI fetches a publication's metadata for confirmation before saving (FR-5.2).
export function lookupDOI(doi: string): Promise<PubResult<PublicationPreview>> {
  return request(() =>
    api.GET("/publications/lookup", { params: { query: { doi } } }),
  );
}

// addFromDOI saves the publication the user confirmed in the preview (FR-5.2, FR-5.4).
export function addFromDOI(doi: string): Promise<PubResult<PublicationAdded>> {
  return request(() =>
    api.POST("/profiles/me/publications/from-doi", { body: { doi } }),
  );
}

// addManualPublication adds a publication by hand (FR-5.1, FR-5.4).
export function addManualPublication(
  body: PublicationCreate,
): Promise<PubResult<PublicationAdded>> {
  return request(() => api.POST("/profiles/me/publications", { body }));
}

// updatePublication changes a publication the user added by hand and shares with no one;
// the API refuses any other with publication_shared_readonly (FR-5.1).
export function updatePublication(
  publicationId: string,
  body: PublicationUpdate,
): Promise<PubResult<Publication>> {
  return request(() =>
    api.PATCH("/profiles/me/publications/{publicationId}", {
      params: { path: { publicationId } },
      body,
    }),
  );
}

// removePublication removes the publication from the user's profile (FR-5.7).
export function removePublication(
  publicationId: string,
): Promise<PubResult<null>> {
  return request(
    () =>
      api.DELETE("/profiles/me/publications/{publicationId}", {
        params: { path: { publicationId } },
      }),
    null,
  );
}

// setMyOrcidId saves the ORCID iD on the profile and answers with its normal form (FR-5.3).
export function setMyOrcidId(
  orcidId: string,
): Promise<PubResult<{ orcidId?: string | null }>> {
  return request(() => api.PUT("/profiles/me/orcid", { body: { orcidId } }));
}

// listMyOrcidWorks lists the public works of the profile's ORCID iD (FR-5.3).
export function listMyOrcidWorks(): Promise<PubResult<OrcidWorkList>> {
  return request(() => api.GET("/profiles/me/publications/orcid-works"));
}

// importOrcidWorks imports the selected works by put-code (FR-5.3, FR-5.4).
export function importOrcidWorks(
  putCodes: string[],
): Promise<PubResult<OrcidImportResult>> {
  return request(() =>
    api.POST("/profiles/me/publications/from-orcid", { body: { putCodes } }),
  );
}
