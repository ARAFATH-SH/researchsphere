import "@/test/dialog-polyfill";

import { fireEvent, render, screen, waitFor } from "@testing-library/react";

import { fakeDate } from "@/test/fake-date";

import { EditPublicationDialog } from "./publication-form";
import { updatePublication } from "./publications-api";
import { failure, publication } from "./test-publications";

jest.mock("./publications-api");
const mockUpdate = jest.mocked(updatePublication);

beforeEach(() => fakeDate("2026-10-15T12:00:00Z"));
afterEach(() => {
  jest.useRealTimers();
  jest.resetAllMocks();
});

const manual = publication(1, { source: "manual", venue: null, abstract: "Old abstract" });

function renderDialog() {
  const onSaved = jest.fn();
  render(<EditPublicationDialog publication={manual} onClose={() => {}} onSaved={onSaved} />);
  return onSaved;
}

describe("EditPublicationDialog", () => {
  it("starts with the publication's values and sends every field", async () => {
    const onSaved = renderDialog();
    expect(screen.getByRole("dialog", { name: "Edit publication" })).toBeTruthy();
    expect(screen.getByRole("textbox", { name: "Title" })).toHaveProperty("value", "Paper 1");
    expect(screen.getByRole("textbox", { name: "Year" })).toHaveProperty("value", "2021");
    mockUpdate.mockResolvedValue({ ok: true, data: manual });

    fireEvent.change(screen.getByRole("textbox", { name: "Title" }), { target: { value: "New title" } });
    fireEvent.change(screen.getByRole("textbox", { name: "Abstract" }), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(mockUpdate).toHaveBeenCalledWith("p1", {
      title: "New title",
      year: 2021,
      venue: null,
      url: null,
      doi: "10.1000/paper-1",
      abstract: null,
    });
  });

  it("says a shared publication cannot be edited", async () => {
    const onSaved = renderDialog();
    mockUpdate.mockResolvedValue(failure("publication_shared_readonly", 409));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect((await screen.findByRole("alert")).textContent).toMatch(/can't be edited/);
    expect(onSaved).not.toHaveBeenCalled();
  });

  it("shows duplicate_doi under the DOI field", async () => {
    renderDialog();
    mockUpdate.mockResolvedValue(failure("duplicate_doi", 409));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("Another publication already has this DOI.")).toBeTruthy();
  });
});
