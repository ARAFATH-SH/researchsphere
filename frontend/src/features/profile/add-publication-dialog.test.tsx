import "@/test/dialog-polyfill";

import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import { fakeDate } from "@/test/fake-date";

import { AddPublicationDialog } from "./add-publication-dialog";
import {
  addFromDOI,
  addManualPublication,
  importOrcidWorks,
  listMyOrcidWorks,
  lookupDOI,
  setMyOrcidId,
} from "./publications-api";
import { failure, preview, publication, work } from "./test-publications";

jest.mock("./publications-api");
const mockLookup = jest.mocked(lookupDOI);
const mockFromDOI = jest.mocked(addFromDOI);
const mockManual = jest.mocked(addManualPublication);
const mockSetOrcid = jest.mocked(setMyOrcidId);
const mockWorks = jest.mocked(listMyOrcidWorks);
const mockImport = jest.mocked(importOrcidWorks);

beforeEach(() => fakeDate("2026-10-15T12:00:00Z"));
afterEach(() => {
  jest.useRealTimers();
  jest.resetAllMocks();
});

function renderDialog(orcidId: string | null = null) {
  const props = {
    onClose: jest.fn(),
    onAdded: jest.fn(),
    onChanged: jest.fn(),
  };
  render(<AddPublicationDialog orcidId={orcidId} {...props} />);
  return props;
}

const type = (name: string | RegExp, value: string) =>
  fireEvent.change(screen.getByRole("textbox", { name }), { target: { value } });
const click = (name: string | RegExp) => fireEvent.click(screen.getByRole("button", { name }));
const tab = (name: string) => fireEvent.click(screen.getByRole("tab", { name }));
const doiField = () => screen.getByRole("textbox", { name: "DOI" });

async function lookUp(doi = "10.1000/found") {
  type("DOI", doi);
  click("Look up");
  await waitFor(() => expect(mockLookup).toHaveBeenCalled());
}

describe("AddPublicationDialog, tabs", () => {
  it("has the design's three tabs, DOI first and focused", () => {
    renderDialog();
    expect(screen.getByRole("dialog", { name: "Add publication" })).toBeTruthy();
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual([
      "Add by DOI",
      "Import from ORCID",
      "Add manually",
    ]);
    const doiTab = screen.getByRole("tab", { name: "Add by DOI" });
    expect(doiTab.getAttribute("aria-selected")).toBe("true");
    expect(document.activeElement).toBe(doiTab);
  });

  it("moves between tabs with the arrow keys", () => {
    renderDialog();
    const first = screen.getByRole("tab", { name: "Add by DOI" });
    fireEvent.keyDown(first, { key: "ArrowLeft" });
    const manual = screen.getByRole("tab", { name: "Add manually" });
    expect(manual.getAttribute("aria-selected")).toBe("true");
    expect(document.activeElement).toBe(manual);
    expect(screen.getByRole("tabpanel", { name: "Add manually" })).toBeTruthy();
  });
});

describe("AddPublicationDialog, Add by DOI", () => {
  it("looks the DOI up, shows the preview and adds it on Confirm", async () => {
    const props = renderDialog();
    mockLookup.mockResolvedValue({ ok: true, data: preview() });
    mockFromDOI.mockResolvedValue({ ok: true, data: { ...publication(9), linked: false } });
    await lookUp("  https://doi.org/10.1000/found ");
    expect(mockLookup).toHaveBeenCalledWith("https://doi.org/10.1000/found");

    const card = await screen.findByRole("region", { name: "Publication found" });
    expect(within(card).getByText("Found on OpenAlex — check before saving")).toBeTruthy();
    expect(within(card).getByText("Early detection from screen-use logs")).toBeTruthy();
    expect(within(card).getByText("Journal of Digital Health · 2026")).toBeTruthy();
    expect(within(card).getByText("Abstract: We collect passive screen-use signals.")).toBeTruthy();
    expect(within(card).queryByText(/already/)).toBeNull();

    fireEvent.click(within(card).getByRole("button", { name: "Confirm & add" }));
    await waitFor(() => expect(props.onAdded).toHaveBeenCalledWith("Publication added"));
    expect(mockFromDOI).toHaveBeenCalledWith("10.1000/found");
  });

  it("cuts a long abstract in the preview", async () => {
    renderDialog();
    mockLookup.mockResolvedValue({ ok: true, data: preview({ abstract: "word ".repeat(200) }) });
    await lookUp();
    const text = (await screen.findByText(/^Abstract:/)).textContent ?? "";
    expect(text.endsWith("…")).toBe(true);
    expect(text.length).toBeLessThan(320);
  });

  it("disables Confirm for a DOI already on the user's profile", async () => {
    renderDialog();
    mockLookup.mockResolvedValue({
      ok: true,
      data: preview({ existsAlready: true, linkedToMe: true, publicationId: "p1" }),
    });
    await lookUp();
    expect(await screen.findByText("This publication is already on your profile.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Confirm & add" })).toHaveProperty("disabled", true);
  });

  it("says the user will be added as an author when another author has it", async () => {
    const props = renderDialog();
    mockLookup.mockResolvedValue({
      ok: true,
      data: preview({ existsAlready: true, linkedToMe: false, publicationId: "p1" }),
    });
    mockFromDOI.mockResolvedValue({ ok: true, data: { ...publication(1), linked: true } });
    await lookUp();
    expect(
      await screen.findByText("This publication is already on ResearchSphere; you'll be added as an author."),
    ).toBeTruthy();
    expect(screen.getByText("Found on ResearchSphere — check before saving")).toBeTruthy();
    click("Confirm & add");
    await waitFor(() => expect(props.onAdded).toHaveBeenCalledWith("Added you as an author"));
  });

  it("Cancel drops the preview", async () => {
    renderDialog();
    mockLookup.mockResolvedValue({ ok: true, data: preview() });
    await lookUp();
    await screen.findByRole("region", { name: "Publication found" });
    click("Cancel");
    expect(screen.queryByRole("region", { name: "Publication found" })).toBeNull();
    expect(mockFromDOI).not.toHaveBeenCalled();
  });

  it("asks for a DOI before looking up", () => {
    renderDialog();
    click("Look up");
    expect(screen.getByText("Enter a DOI.")).toBeTruthy();
    expect(mockLookup).not.toHaveBeenCalled();
  });

  it.each([
    ["invalid_doi", 400, "Enter a DOI such as 10.1145/3292500.3330701."],
    ["doi_not_found", 404, "We couldn't find a publication with this DOI. Check it, or add the publication by hand."],
  ])("shows %s under the DOI field", async (code, status, message) => {
    renderDialog();
    mockLookup.mockResolvedValue(failure(code, status));
    await lookUp();
    expect(await screen.findByText(message)).toBeTruthy();
    expect(doiField().getAttribute("aria-invalid")).toBe("true");
  });

  it("says in minutes when lookups are allowed again", async () => {
    renderDialog();
    mockLookup.mockResolvedValue(failure("rate_limited", 429, "x", 600));
    await lookUp();
    expect((await screen.findByRole("alert")).textContent).toBe(
      "You've made too many requests. Try again in 10 minutes.",
    );
  });

  it("says when the limit of 300 is reached on Confirm", async () => {
    renderDialog();
    mockLookup.mockResolvedValue({ ok: true, data: preview() });
    mockFromDOI.mockResolvedValue(failure("publication_limit_reached", 409));
    await lookUp();
    click("Confirm & add");
    expect((await screen.findByRole("alert")).textContent).toMatch(/limit of 300 publications/);
  });

  it("FR-5.6: when OpenAlex cannot be reached, offers manual entry with the DOI filled in", async () => {
    renderDialog();
    const message = "We couldn't reach the publication database. You can add this publication by hand.";
    mockLookup.mockResolvedValue(failure("metadata_unavailable", 503, message));
    await lookUp("10.1000/offline");
    expect(await screen.findByText(message)).toBeTruthy();

    click("Add manually instead");
    expect(screen.getByRole("tab", { name: "Add manually" }).getAttribute("aria-selected")).toBe("true");
    const panel = screen.getByRole("tabpanel", { name: "Add manually" });
    expect(within(panel).getByText(message)).toBeTruthy();
    expect(within(panel).getByRole("textbox", { name: "DOI (optional)" })).toHaveProperty(
      "value",
      "10.1000/offline",
    );
  });
});

describe("AddPublicationDialog, Import from ORCID", () => {
  it("saves the ORCID iD first when the profile has none, then lists the works", async () => {
    const props = renderDialog(null);
    tab("Import from ORCID");
    mockSetOrcid.mockResolvedValue({ ok: true, data: { orcidId: "0000-0002-1825-0097" } });
    mockWorks.mockResolvedValue({ ok: true, data: { orcidId: "0000-0002-1825-0097", items: [work("1")] } });
    type("ORCID iD", " https://orcid.org/0000-0002-1825-0097 ");
    click("Save & list my works");

    expect(await screen.findByRole("checkbox", { name: "Work 1 · 2025" })).toBeTruthy();
    expect(mockSetOrcid).toHaveBeenCalledWith("https://orcid.org/0000-0002-1825-0097");
    expect(props.onChanged).toHaveBeenCalled();
    expect(screen.getByText("0000-0002-1825-0097")).toBeTruthy();
    expect(screen.queryByRole("textbox", { name: "ORCID iD" })).toBeNull();
  });

  it.each([
    ["invalid_orcid_id", 400, "Enter an ORCID iD such as 0000-0002-1825-0097."],
    ["orcid_taken", 409, "Another profile already has this ORCID iD."],
  ])("shows %s under the ORCID iD field", async (code, status, message) => {
    renderDialog(null);
    tab("Import from ORCID");
    mockSetOrcid.mockResolvedValue(failure(code, status));
    type("ORCID iD", "0000-0000-0000-0000");
    click("Save & list my works");
    expect(await screen.findByText(message)).toBeTruthy();
    expect(screen.getByRole("textbox", { name: "ORCID iD" }).getAttribute("aria-invalid")).toBe("true");
    expect(mockWorks).not.toHaveBeenCalled();
  });

  it("with a saved iD, lists the works and imports only the ticked ones", async () => {
    const props = renderDialog("0000-0002-1825-0097");
    tab("Import from ORCID");
    expect(screen.queryByRole("textbox", { name: "ORCID iD" })).toBeNull();
    mockWorks.mockResolvedValue({
      ok: true,
      data: {
        orcidId: "0000-0002-1825-0097",
        items: [
          work("1", { title: "Wearable blink sensing" }),
          work("2", { title: "A survey", year: null }),
          work("3", { title: "Explainable CVS", year: 2026, existsAlready: true, linkedToMe: true }),
        ],
      },
    });
    mockImport.mockResolvedValue({
      ok: true,
      data: { items: [], created: 1, linked: 1, skipped: 0 },
    });
    click("List my works");

    const mine = await screen.findByRole("checkbox", {
      name: "Explainable CVS · 2026 · already on your profile",
    });
    expect(mine).toHaveProperty("checked", true);
    expect(mine).toHaveProperty("disabled", true);
    expect(screen.getByRole("button", { name: "Import 0 selected" })).toHaveProperty("disabled", true);

    fireEvent.click(screen.getByRole("checkbox", { name: "Wearable blink sensing · 2025" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "A survey" }));
    click("Import 2 selected");

    const done = await screen.findByRole("status");
    expect(done.textContent).toBe(
      "Import finished. 1 added, 1 linked to an existing record, 0 skipped (already on your profile).Done",
    );
    expect(mockImport).toHaveBeenCalledWith(["1", "2"]);
    expect(props.onChanged).toHaveBeenCalled();
    expect(screen.queryByRole("checkbox")).toBeNull();
    click("Done");
    expect(props.onClose).toHaveBeenCalled();
  });

  it("shows orcid_not_found by the iD, and ORCID's own message when it is unreachable", async () => {
    renderDialog("0000-0002-1825-0097");
    tab("Import from ORCID");
    mockWorks.mockResolvedValueOnce(failure("orcid_not_found", 404));
    click("List my works");
    expect(await screen.findByText("ORCID has no record with this iD. Check the iD you saved.")).toBeTruthy();

    const message = "We couldn't reach ORCID. You can still add publications by DOI or by hand.";
    mockWorks.mockResolvedValueOnce(failure("metadata_unavailable", 503, message));
    click("List my works");
    expect(await screen.findByText(message)).toBeTruthy();
  });

  it("says when ORCID calls are rate limited", async () => {
    renderDialog("0000-0002-1825-0097");
    tab("Import from ORCID");
    mockWorks.mockResolvedValue(failure("rate_limited", 429, "x", 3000));
    click("List my works");
    expect(await screen.findByText("You've made too many requests. Try again in 50 minutes.")).toBeTruthy();
  });
});

describe("AddPublicationDialog, Add manually", () => {
  function fill() {
    tab("Add manually");
    type("Title", " A hand-made paper ");
    type("Year", "2024");
    type("Venue", "KDD");
    type("Abstract", "Short.");
  }

  it("adds a publication with the fields trimmed and empty ones null", async () => {
    const props = renderDialog();
    mockManual.mockResolvedValue({ ok: true, data: { ...publication(1), linked: false } });
    fill();
    expect(screen.getByText("6 / 5000")).toBeTruthy();
    click("Add publication");
    await waitFor(() => expect(props.onAdded).toHaveBeenCalledWith("Publication added"));
    expect(mockManual).toHaveBeenCalledWith({
      title: "A hand-made paper",
      year: 2024,
      venue: "KDD",
      url: null,
      doi: null,
      abstract: "Short.",
    });
  });

  it("checks title and year before sending", () => {
    renderDialog();
    tab("Add manually");
    click("Add publication");
    expect(screen.getByText("Enter the title.")).toBeTruthy();
    expect(screen.getByText("Enter the year.")).toBeTruthy();
    expect(mockManual).not.toHaveBeenCalled();
  });

  it("shows a refused DOI under the DOI field", async () => {
    renderDialog();
    mockManual.mockResolvedValue(failure("publication_already_added", 409));
    fill();
    type("DOI (optional)", "10.1000/mine");
    click("Add publication");
    expect(await screen.findByText("This publication is already on your profile.")).toBeTruthy();
    expect(screen.getByRole("textbox", { name: "DOI (optional)" }).getAttribute("aria-invalid")).toBe("true");
  });

  it("asks before closing with unsaved text", () => {
    const props = renderDialog();
    fill();
    click("Close");
    expect(screen.getByText("Discard changes?")).toBeTruthy();
    expect(props.onClose).not.toHaveBeenCalled();
  });
});
