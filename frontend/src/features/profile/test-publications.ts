import type {
  OrcidWork,
  PubFailure,
  Publication,
  PublicationPage,
  PublicationPreview,
} from "./publications-api";

// Publications shared by the publication tests.

export function publication(n: number, over: Partial<Publication> = {}): Publication {
  return {
    id: `p${n}`,
    doi: `10.1000/paper-${n}`,
    title: `Paper ${n}`,
    year: 2020 + n,
    venue: `Venue ${n}`,
    abstract: null,
    url: null,
    source: "doi",
    createdAt: "2026-10-01T00:00:00Z",
    updatedAt: "2026-10-01T00:00:00Z",
    ...over,
  };
}

export function page(items: Publication[], total = items.length, n = 1, pageSize = 3): PublicationPage {
  return { items, total, page: n, pageSize };
}

export function preview(over: Partial<PublicationPreview> = {}): PublicationPreview {
  return {
    doi: "10.1000/found",
    title: "Early detection from screen-use logs",
    year: 2026,
    venue: "Journal of Digital Health",
    abstract: "We collect passive screen-use signals.",
    url: "https://doi.org/10.1000/found",
    existsAlready: false,
    linkedToMe: false,
    ...over,
  };
}

export function work(putCode: string, over: Partial<OrcidWork> = {}): OrcidWork {
  return {
    putCode,
    title: `Work ${putCode}`,
    year: 2025,
    venue: null,
    doi: null,
    type: "journal-article",
    url: null,
    existsAlready: false,
    linkedToMe: false,
    ...over,
  };
}

export function failure(code: string, status: number, message = "server text", retryAfter?: number): PubFailure {
  return { ok: false, code, status, message, retryAfter };
}
