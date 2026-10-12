import { act, render, screen } from "@testing-library/react";
import * as React from "react";

import type { getCurrentUser as GetCurrentUser } from "./api";

// Each test loads a fresh copy of the store, since it keeps module state.
function load(me: Awaited<ReturnType<typeof GetCurrentUser>>) {
  let lost: () => void = () => {};
  const getCurrentUser = jest.fn().mockResolvedValue(me);
  jest.doMock("./api", () => ({ getCurrentUser }));
  jest.doMock("@/lib/api/client", () => ({
    onSessionLost: (listener: () => void) => {
      lost = listener;
      return () => {};
    },
  }));
  // The fresh copy must share the React that renders it.
  jest.doMock("react", () => React);
  let mod!: typeof import("./session");
  jest.isolateModules(() => {
    mod = jest.requireActual<typeof import("./session")>("./session");
  });
  function Show() {
    const s = mod.useSession();
    return <p>{s.status === "signedIn" ? s.user.fullName : s.status}</p>;
  }
  return { mod, getCurrentUser, Show, loseSession: () => lost() };
}

const user = { id: "0b8f4c1e-0000-4000-8000-000000000001", email: "ada@example.org", fullName: "Ada Lovelace" };

describe("session", () => {
  afterEach(() => jest.resetModules());

  it("asks /auth/me once, however many components read it", async () => {
    const { getCurrentUser, Show } = load({ ok: true, data: user });
    render(
      <>
        <Show />
        <Show />
      </>,
    );
    expect(await screen.findAllByText("Ada Lovelace")).toHaveLength(2);
    expect(getCurrentUser).toHaveBeenCalledTimes(1);
  });

  it("is signed out when /auth/me fails", async () => {
    const { Show } = load({ ok: false, code: "unauthenticated", message: "Sign in first." });
    render(<Show />);
    expect(await screen.findByText("signedOut")).toBeTruthy();
  });

  it("becomes signed out when the session cannot be refreshed", async () => {
    const { Show, loseSession } = load({ ok: true, data: user });
    render(<Show />);
    await screen.findByText("Ada Lovelace");
    act(() => loseSession());
    expect(screen.getByText("signedOut")).toBeTruthy();
  });

  it("follows login and logout", async () => {
    const { mod, Show } = load({ ok: false, code: "unauthenticated", message: "Sign in first." });
    render(<Show />);
    await screen.findByText("signedOut");
    act(() => mod.setSignedIn(user));
    expect(screen.getByText("Ada Lovelace")).toBeTruthy();
    act(() => mod.setSignedOut());
    expect(screen.getByText("signedOut")).toBeTruthy();
  });
});
