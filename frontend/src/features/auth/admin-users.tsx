"use client";

import { useEffect, useState } from "react";

import { SectionCard } from "@/components/section-layout";
import { rsButtonClass } from "@/components/ui/button";
import { FormError } from "@/components/ui/field";

import { type AdminUser, listUsers, type UserPage } from "./api";

export const usersPageSize = 20;

type State = { status: "loading" } | { status: "error"; message: string } | { status: "loaded"; data: UserPage };

const joinedFormat = new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short", year: "numeric", timeZone: "UTC" });

// AdminUsers is the administrator's read-only list of accounts (FR-15.3), a page at a
// time. Suspending an account comes with slice 27.
export function AdminUsers() {
  const [page, setPage] = useState(1);
  const [state, setState] = useState<State>({ status: "loading" });

  useEffect(() => {
    let current = true;
    void listUsers(page, usersPageSize).then((res) => {
      if (!current) return;
      setState(res.ok ? { status: "loaded", data: res.data } : { status: "error", message: res.message });
    });
    return () => {
      current = false;
    };
  }, [page]);

  const go = (next: number) => {
    setState({ status: "loading" });
    setPage(next);
  };

  const count =
    state.status === "loaded" ? (
      <span className="text-rs-caption text-rs-text-muted">
        {state.data.total} {state.data.total === 1 ? "account" : "accounts"}
      </span>
    ) : null;

  return (
    <SectionCard id="users" title="Users" header aside={count}>
      {state.status === "loading" && (
        <p role="status" className="px-4 py-3 text-rs-body text-rs-text-muted">
          Loading users…
        </p>
      )}
      {state.status === "error" && (
        <div className="px-4 py-3">
          <FormError look="rs">{state.message}</FormError>
        </div>
      )}
      {state.status === "loaded" && <UsersTable data={state.data} onPage={go} />}
    </SectionCard>
  );
}

// Cells as in the design's tables: 8px apart, 16px from the card's edges.
const cellClass = "px-2 first:pl-4 last:pr-4";

const chipClass = "inline-block rounded-rs-pill px-2.5 py-[3px] text-rs-caption font-medium whitespace-nowrap";

function UsersTable({ data, onPage }: { data: UserPage; onPage: (page: number) => void }) {
  const pages = Math.max(1, Math.ceil(data.total / data.pageSize));
  const pageButton = rsButtonClass({ variant: "secondary", size: "sm" });
  return (
    <>
      {/* At narrow widths the table scrolls inside the card instead of widening the page. */}
      <div className="overflow-x-auto py-2">
        <table className="w-full min-w-[640px] border-collapse text-left text-rs-body text-rs-text">
          <thead className="text-rs-caption text-rs-text-muted">
            <tr>
              {["Name", "Email", "Role", "Verified", "Status", "Joined"].map((h) => (
                <th key={h} scope="col" className={`${cellClass} py-2 font-medium`}>
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {data.items.length === 0 ? (
              <tr className="border-t border-rs-divider">
                <td colSpan={6} className={`${cellClass} py-2.5 text-rs-text-muted`}>
                  No users on this page.
                </td>
              </tr>
            ) : (
              data.items.map((u) => <UserRow key={u.id} user={u} />)
            )}
          </tbody>
        </table>
      </div>
      <nav aria-label="Pages" className="flex items-center justify-between gap-4 border-t border-rs-divider px-4 py-3">
        <button type="button" className={pageButton} disabled={data.page <= 1} onClick={() => onPage(data.page - 1)}>
          Previous
        </button>
        <span className="text-rs-caption text-rs-text-muted">
          Page {data.page} of {pages}
        </span>
        <button type="button" className={pageButton} disabled={data.page >= pages} onClick={() => onPage(data.page + 1)}>
          Next
        </button>
      </nav>
    </>
  );
}

function UserRow({ user }: { user: AdminUser }) {
  return (
    <tr className="border-t border-rs-divider hover:bg-rs-subtle">
      <td className={`${cellClass} py-2.5 font-medium`}>{user.fullName}</td>
      <td className={`${cellClass} py-2.5 break-all`}>{user.email}</td>
      <td className={`${cellClass} py-2.5`}>
        {user.role === "admin" ? (
          <span className={`${chipClass} bg-rs-violet-soft text-rs-violet`}>Admin</span>
        ) : (
          <span className={`${chipClass} bg-rs-neutral text-rs-text`}>Researcher</span>
        )}
      </td>
      <td className={`${cellClass} py-2.5`}>{user.verified ? "Yes" : "No"}</td>
      <td className={`${cellClass} py-2.5`}>
        {user.suspended ? (
          <span className={`${chipClass} bg-rs-danger-soft text-rs-danger-strong`}>Suspended</span>
        ) : (
          <span className={`${chipClass} bg-rs-accent-soft text-rs-heading`}>Active</span>
        )}
      </td>
      <td className={`${cellClass} py-2.5 whitespace-nowrap`}>
        <time dateTime={user.createdAt}>{joinedFormat.format(new Date(user.createdAt))}</time>
      </td>
    </tr>
  );
}
