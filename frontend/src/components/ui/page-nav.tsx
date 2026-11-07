import { rsButtonClass } from "./button";

type PageNavProps = { page: number; pageSize: number; total: number; onPage: (page: number) => void };

// PageNav is the row under a paged admin list: Previous, "Page 1 of 3", Next.
export function PageNav({ page, pageSize, total, onPage }: PageNavProps) {
  const pages = Math.max(1, Math.ceil(total / pageSize));
  const button = rsButtonClass({ variant: "secondary", size: "sm" });
  return (
    <nav aria-label="Pages" className="flex items-center justify-between gap-4 border-t border-rs-divider px-4 py-3">
      <button type="button" className={button} disabled={page <= 1} onClick={() => onPage(page - 1)}>
        Previous
      </button>
      <span className="text-rs-caption text-rs-text-muted">
        Page {page} of {pages}
      </span>
      <button type="button" className={button} disabled={page >= pages} onClick={() => onPage(page + 1)}>
        Next
      </button>
    </nav>
  );
}
