import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import {
  listMyPublications,
  listPublications,
  removePublication,
} from "./publications-api";
import { type PublicationControls, PublicationsSection } from "./publications-section";
import { adaID, bobID, emptyProfile } from "./test-profiles";
import { failure, page, publication } from "./test-publications";

jest.mock("./publications-api");
const mockListMine = jest.mocked(listMyPublications);
const mockList = jest.mocked(listPublications);
const mockRemove = jest.mocked(removePublication);

afterEach(() => jest.resetAllMocks());

function profileWith(userId: string, count: number) {
  return { ...emptyProfile(userId, "Someone"), publicationCount: count };
}

function controls(): PublicationControls {
  return { add: jest.fn(), edit: jest.fn(), removed: jest.fn() };
}

const items = () => within(screen.getByRole("list", { name: "Publications" })).getAllByRole("listitem");

describe("PublicationsSection, someone else's profile", () => {
  it("shows title, venue · year and the DOI as a doi.org link in a new tab", async () => {
    mockList.mockResolvedValue({
      ok: true,
      data: page([
        publication(1),
        publication(2, { year: null }),
        publication(3, { venue: null }),
      ]),
    });
    render(<PublicationsSection profile={profileWith(bobID, 3)} />);

    const [first, second, third] = await screen.findAllByRole("listitem");
    expect(mockList).toHaveBeenCalledWith(bobID, 1, 3);
    expect(within(first).getByText("Paper 1")).toBeTruthy();
    expect(within(first).getByText("Venue 1 · 2021")).toBeTruthy();
    expect(within(second).getByText("Venue 2")).toBeTruthy();
    expect(within(third).getByText("2023")).toBeTruthy();
    const link = within(first).getByRole("link", { name: "doi:10.1000/paper-1" });
    expect(link.getAttribute("href")).toBe("https://doi.org/10.1000/paper-1");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toBe("noopener");
    expect(screen.queryByRole("button", { name: /more actions/i })).toBeNull();
    expect(screen.queryByRole("button", { name: "Add publication" })).toBeNull();
  });

  it("leaves out the meta line with neither venue nor year, and links a record without a DOI to its own link", async () => {
    mockList.mockResolvedValue({
      ok: true,
      data: page([publication(1, { venue: null, year: null, doi: null, url: "https://example.org/p" })]),
    });
    render(<PublicationsSection profile={profileWith(bobID, 1)} />);
    const [item] = await screen.findAllByRole("listitem");
    expect(item.querySelectorAll("p")).toHaveLength(1);
    expect(within(item).getByRole("link", { name: "View publication" }).getAttribute("href")).toBe(
      "https://example.org/p",
    );
  });

  it("clamps a long abstract with …see more", async () => {
    mockList.mockResolvedValue({ ok: true, data: page([publication(1, { abstract: "word ".repeat(80) })]) });
    render(<PublicationsSection profile={profileWith(bobID, 1)} />);
    const more = await screen.findByRole("button", { name: "…see more" });
    fireEvent.click(more);
    expect(screen.getByRole("button", { name: "see less" })).toBeTruthy();
  });

  it("links to all publications when there are more than three", async () => {
    mockList.mockResolvedValue({
      ok: true,
      data: page([publication(1), publication(2), publication(3)], 7),
    });
    render(<PublicationsSection profile={profileWith(bobID, 7)} />);
    const all = await screen.findByRole("link", { name: /Show all 7 publications/ });
    expect(all.getAttribute("href")).toBe(`/profile/${bobID}/publications`);
    expect(items()).toHaveLength(3);
  });

  it("has no Show all with three or fewer", async () => {
    mockList.mockResolvedValue({ ok: true, data: page([publication(1), publication(2), publication(3)]) });
    render(<PublicationsSection profile={profileWith(bobID, 3)} />);
    await screen.findAllByRole("listitem");
    expect(screen.queryByRole("link", { name: /show all/i })).toBeNull();
  });

  it("is hidden without publications, and asks for none", () => {
    const { container } = render(<PublicationsSection profile={profileWith(bobID, 0)} />);
    expect(container.textContent).toBe("");
    expect(mockList).not.toHaveBeenCalled();
  });

  it("offers to try again when the list does not load", async () => {
    mockList.mockResolvedValueOnce(failure("internal_error", 500));
    mockList.mockResolvedValueOnce({ ok: true, data: page([publication(1)]) });
    render(<PublicationsSection profile={profileWith(bobID, 1)} />);
    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Paper 1")).toBeTruthy();
  });
});

describe("PublicationsSection, own profile", () => {
  it("shows the empty state with Add publication", () => {
    const c = controls();
    render(<PublicationsSection profile={profileWith(adaID, 0)} controls={c} />);
    expect(screen.getByRole("heading", { name: "Publications" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Add publication" }).getAttribute("href")).toBe(
      "/profile?edit=publications",
    );
    fireEvent.click(screen.getByRole("button", { name: "Add publication" }));
    expect(c.add).toHaveBeenCalled();
    expect(mockListMine).not.toHaveBeenCalled();
  });

  it("lists the own publications with a menu each; Edit only for a manual record", async () => {
    const c = controls();
    const manual = publication(1, { source: "manual" });
    mockListMine.mockResolvedValue({ ok: true, data: page([manual, publication(2, { source: "orcid" })]) });
    render(<PublicationsSection profile={profileWith(adaID, 2)} controls={c} />);
    await screen.findAllByRole("listitem");
    expect(mockListMine).toHaveBeenCalledWith(1, 3);

    fireEvent.click(screen.getByRole("button", { name: "More actions for Paper 2" }));
    expect(screen.getByRole("menuitem", { name: "Remove" })).toBeTruthy();
    expect(screen.queryByRole("menuitem", { name: "Edit" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "More actions for Paper 1" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Edit" }));
    expect(c.edit).toHaveBeenCalledWith(manual);
  });

  it("removes a publication only after the confirmation", async () => {
    const c = controls();
    mockListMine.mockResolvedValue({ ok: true, data: page([publication(1)]) });
    mockRemove.mockResolvedValue({ ok: true, data: null });
    render(<PublicationsSection profile={profileWith(adaID, 1)} controls={c} />);
    fireEvent.click(await screen.findByRole("button", { name: "More actions for Paper 1" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Remove" }));

    const confirm = screen.getByRole("group", { name: "Remove publication" });
    expect(within(confirm).getByText("Remove this publication from your profile?")).toBeTruthy();
    expect(mockRemove).not.toHaveBeenCalled();
    fireEvent.click(within(confirm).getByRole("button", { name: "Cancel" }));
    expect(screen.getByRole("menuitem", { name: "Remove" })).toBeTruthy();

    fireEvent.click(screen.getByRole("menuitem", { name: "Remove" }));
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(c.removed).toHaveBeenCalled());
    expect(mockRemove).toHaveBeenCalledWith("p1");
  });

  it("says so when removing fails, and keeps the publication", async () => {
    const c = controls();
    mockListMine.mockResolvedValue({ ok: true, data: page([publication(1)]) });
    mockRemove.mockResolvedValue(failure("internal_error", 500));
    render(<PublicationsSection profile={profileWith(adaID, 1)} controls={c} />);
    fireEvent.click(await screen.findByRole("button", { name: "More actions for Paper 1" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Remove" }));
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    expect((await screen.findByRole("alert")).textContent).toMatch(/couldn't remove/i);
    expect(c.removed).not.toHaveBeenCalled();
  });

  it("fetches again when the count or refresh key changes", async () => {
    mockListMine.mockResolvedValue({ ok: true, data: page([publication(1)]) });
    const c = controls();
    const { rerender } = render(<PublicationsSection profile={profileWith(adaID, 1)} controls={c} />);
    await screen.findByText("Paper 1");
    mockListMine.mockResolvedValue({ ok: true, data: page([publication(2), publication(1)]) });
    rerender(<PublicationsSection profile={profileWith(adaID, 2)} controls={c} />);
    expect(await screen.findByText("Paper 2")).toBeTruthy();
    mockListMine.mockResolvedValue({ ok: true, data: page([publication(2, { title: "Renamed" }), publication(1)]) });
    rerender(<PublicationsSection profile={profileWith(adaID, 2)} refreshKey={1} controls={c} />);
    expect(await screen.findByText("Renamed")).toBeTruthy();
    expect(mockListMine).toHaveBeenCalledTimes(3);
  });
});
