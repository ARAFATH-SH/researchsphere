import { fireEvent, render, screen, within } from "@testing-library/react";

import { PublicationListPage } from "./publication-list-page";
import { listPublications } from "./publications-api";
import { bobID } from "./test-profiles";
import { failure, page, publication } from "./test-publications";

jest.mock("./publications-api");
const mockList = jest.mocked(listPublications);

afterEach(() => jest.resetAllMocks());

const titles = () =>
  within(screen.getByRole("list", { name: "Publications" }))
    .getAllByRole("listitem")
    .map((li) => li.querySelector("p")?.textContent);

describe("PublicationListPage", () => {
  it("pages through all publications with the API's total", async () => {
    const first = Array.from({ length: 10 }, (_, i) => publication(i + 1));
    mockList.mockResolvedValueOnce({ ok: true, data: page(first, 12, 1, 10) });
    mockList.mockResolvedValueOnce({ ok: true, data: page([publication(11), publication(12)], 12, 2, 10) });
    render(<PublicationListPage userId={bobID} />);

    expect(await screen.findByRole("heading", { name: "Publications (12)" })).toBeTruthy();
    expect(titles()).toHaveLength(10);
    expect(screen.getByText("Page 1 of 2")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Previous" })).toHaveProperty("disabled", true);
    expect(mockList).toHaveBeenCalledWith(bobID, 1, 10);

    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByText("Page 2 of 2")).toBeTruthy();
    expect(titles()).toEqual(["Paper 11", "Paper 12"]);
    expect(screen.getByRole("button", { name: "Next" })).toHaveProperty("disabled", true);
    expect(mockList).toHaveBeenLastCalledWith(bobID, 2, 10);
    expect(screen.getByRole("link", { name: /Back to profile/ }).getAttribute("href")).toBe(`/profile/${bobID}`);
    // Read-only: no menus here.
    expect(screen.queryByRole("button", { name: /more actions/i })).toBeNull();
  });

  it("has no paging for a single page", async () => {
    mockList.mockResolvedValue({ ok: true, data: page([publication(1)], 1, 1, 10) });
    render(<PublicationListPage userId={bobID} />);
    await screen.findByText("Paper 1");
    expect(screen.queryByRole("navigation", { name: "Pages" })).toBeNull();
  });

  it("says when the profile is not available", async () => {
    mockList.mockResolvedValue(failure("not_found", 404));
    render(<PublicationListPage userId="nobody" />);
    expect(await screen.findByText("This profile does not exist or is not available.")).toBeTruthy();
  });
});
