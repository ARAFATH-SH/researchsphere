"use client";

import { useEffect, useState } from "react";

import { buttonClass } from "@/components/ui/button";
import { Card } from "@/components/ui/card";

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

  return (
    <Card className="p-4 md:p-6">
      <h1 className="text-xl font-semibold text-text">Users</h1>
      {state.status === "loading" && (
        <p role="status" className="mt-4 text-sm text-text-muted">
          Loading users…
        </p>
      )}
      {state.status === "error" && (
        <p role="alert" className="mt-4 rounded-lg border border-danger px-4 py-3 text-sm text-danger">
          {state.message}
        </p>
      )}
      {state.status === "loaded" && <UsersTable data={state.data} onPage={go} />}
    </Card>
  );
}

function UsersTable({ data, onPage }: { data: UserPage; onPage: (page: number) => void }) {
  const pages = Math.max(1, Math.ceil(data.total / data.pageSize));
  const pageButton = buttonClass({ variant: "outline", size: "md" });
  return (
    <>
      <p className="mt-1 text-sm text-text-muted">
        {data.total} {data.total === 1 ? "account" : "accounts"}
      </p>
      {/* At narrow widths the table scrolls inside the card instead of widening the page. */}
      <div className="mt-4 overflow-x-auto">
        <table className="w-full min-w-[640px] text-left text-sm">
          <thead className="border-b border-border text-text-muted">
            <tr>
              {["Name", "Email", "Role", "Verified", "Suspended", "Joined"].map((h) => (
                <th key={h} scope="col" className="py-2 pr-4 font-semibold last:pr-0">
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {data.items.length === 0 ? (
              <tr>
                <td colSpan={6} className="py-4 text-text-muted">
                  No users on this page.
                </td>
              </tr>
            ) : (
              data.items.map((u) => <UserRow key={u.id} user={u} />)
            )}
          </tbody>
        </table>
      </div>
      <nav aria-label="Pages" className="mt-4 flex items-center justify-between gap-4">
        <button type="button" className={pageButton} disabled={data.page <= 1} onClick={() => onPage(data.page - 1)}>
          Previous
        </button>
        <span className="text-sm text-text-muted">
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
    <tr className="border-b border-border last:border-0">
      <td className="py-2 pr-4 text-text">{user.fullName}</td>
      <td className="py-2 pr-4 break-all text-text">{user.email}</td>
      <td className="py-2 pr-4 text-text">{user.role === "admin" ? "Admin" : "Researcher"}</td>
      <td className="py-2 pr-4 text-text">{user.verified ? "Yes" : "No"}</td>
      <td className={`py-2 pr-4 ${user.suspended ? "font-semibold text-danger" : "text-text"}`}>
        {user.suspended ? "Yes" : "No"}
      </td>
      <td className="py-2 text-text">
        <time dateTime={user.createdAt}>{joinedFormat.format(new Date(user.createdAt))}</time>
      </td>
    </tr>
  );
}
