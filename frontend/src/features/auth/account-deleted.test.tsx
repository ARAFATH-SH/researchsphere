import { render, screen } from "@testing-library/react";

import { AccountDeletedNotice, markAccountDeleted } from "./account-deleted";

describe("AccountDeletedNotice", () => {
  afterEach(() => sessionStorage.clear());

  it("shows nothing when no account was deleted", () => {
    render(<AccountDeletedNotice />);
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("says the account was deleted, once", async () => {
    markAccountDeleted();
    const { unmount } = render(<AccountDeletedNotice />);
    expect((await screen.findByRole("status")).textContent).toBe("Your account was deleted.");
    unmount();

    render(<AccountDeletedNotice />);
    expect(screen.queryByRole("status")).toBeNull();
  });
});
