"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import type { FormEvent } from "react";

import { SearchIcon } from "./icons";

const searchStyles = {
  // Up to 280px; between 641 and 760px only the icon shows until the field has the focus;
  // from 481 to 640px it fills the space beside the logo.
  top: {
    box: "h-[34px] flex-[0_1_280px] rounded-rs-xs px-2.5 max-[760px]:flex-[0_1_40px] max-[760px]:focus-within:flex-[0_1_280px] max-[640px]:flex-auto max-[640px]:focus-within:flex-auto",
    input: "max-[760px]:placeholder:text-transparent max-[640px]:placeholder:text-rs-nav-muted",
    placeholder: "Search researchers, projects, posts",
  },
  phone: { box: "h-9 flex-1 rounded-rs-pill px-3", input: "", placeholder: "Search researchers, projects" },
};

export type SearchBoxVariant = keyof typeof searchStyles;

// minQueryLength is the shortest query the search API accepts (FR-6.1).
export const minQueryLength = 2;

type SearchBoxProps = { id: string; variant: SearchBoxVariant; value?: string };

// StaticSearchBox is the search box before the URL is known (prerender): a plain form that
// still sends the query to /search.
export function StaticSearchBox({ id, variant, value = "", onSubmit }: SearchBoxProps & {
  onSubmit?: (event: FormEvent<HTMLFormElement>) => void;
}) {
  const style = searchStyles[variant];
  return (
    <form
      action="/search"
      role="search"
      onSubmit={onSubmit}
      className={`flex min-w-0 items-center gap-2 bg-rs-search text-rs-body text-rs-nav-muted focus-within:shadow-focus ${style.box}`}
    >
      <label htmlFor={id} className="flex flex-none">
        <SearchIcon />
        <span className="sr-only">Search</span>
      </label>
      <input
        // Keyed by the value, so the box shows the new query after back or forward.
        key={value}
        id={id}
        name="q"
        type="search"
        maxLength={100}
        defaultValue={value}
        placeholder={style.placeholder}
        className={`min-w-0 flex-1 bg-transparent text-rs-nav-text outline-none placeholder:text-rs-nav-muted ${style.input}`}
      />
    </form>
  );
}

// SearchBox sends the trimmed query to the search page (FR-6.1); shorter than 2 characters
// does nothing. On the search page it shows the current query.
export function SearchBox({ id, variant }: Omit<SearchBoxProps, "value">) {
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const value = pathname === "/search" ? (params.get("q") ?? "") : "";

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const q = String(new FormData(event.currentTarget).get("q") ?? "").trim();
    if (q.length < minQueryLength) return;
    router.push(`/search?${new URLSearchParams({ q })}`);
  }

  return <StaticSearchBox id={id} variant={variant} value={value} onSubmit={submit} />;
}
