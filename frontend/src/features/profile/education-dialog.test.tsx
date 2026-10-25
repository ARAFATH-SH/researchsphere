import "@/test/dialog-polyfill";

import { fireEvent, render, screen } from "@testing-library/react";

import { addEducation, removeEducation, updateEducation } from "./api";
import { EducationDialog } from "./education-dialog";
import { adaID, fullProfile } from "./test-profiles";
import { educationMessages } from "./validation";

jest.mock("./api");
const mockAdd = jest.mocked(addEducation);
const mockUpdate = jest.mocked(updateEducation);
const mockRemove = jest.mocked(removeEducation);
afterEach(() => jest.resetAllMocks());

function renderEducation() {
  const onSaved = jest.fn();
  render(
    <EducationDialog
      profile={fullProfile(adaID, "Ada Lovelace")}
      onClose={() => {}}
      onSaved={onSaved}
    />,
  );
  return onSaved;
}

const type = (name: string, value: string) =>
  fireEvent.change(screen.getByRole("textbox", { name }), {
    target: { value },
  });
const save = () =>
  fireEvent.click(screen.getByRole("button", { name: "Save" }));

describe("EducationDialog", () => {
  it("adds a current entry with endYear null", async () => {
    mockAdd.mockResolvedValue({ ok: true, data: undefined });
    const onSaved = renderEducation();
    fireEvent.click(screen.getByRole("button", { name: "Add education" }));
    expect(screen.getByRole("dialog", { name: "Add education" })).toBeTruthy();

    type("Institution", "  Oxford ");
    type("Start year", "2024");
    type("End year", "2027");
    fireEvent.click(
      screen.getByRole("checkbox", { name: "I currently study here" }),
    );
    const endYear = screen.getByRole("textbox", {
      name: "End year",
    }) as HTMLInputElement;
    expect(endYear.disabled).toBe(true);
    expect(endYear.value).toBe("");

    save();
    await screen.findByRole("button", { name: "Save" });
    expect(mockAdd).toHaveBeenCalledWith({
      institution: "Oxford",
      degree: null,
      fieldOfStudy: null,
      startYear: 2024,
      endYear: null,
    });
    expect(onSaved).toHaveBeenCalledWith();
  });

  it("edits an entry: ticking current sends endYear null in the PATCH", async () => {
    mockUpdate.mockResolvedValue({ ok: true, data: undefined });
    renderEducation();
    fireEvent.click(screen.getByRole("button", { name: "Edit MIT" }));
    expect(
      (screen.getByRole("textbox", { name: "End year" }) as HTMLInputElement)
        .value,
    ).toBe("2026");
    fireEvent.click(
      screen.getByRole("checkbox", { name: "I currently study here" }),
    );
    save();
    await screen.findByRole("button", { name: "Save" });
    expect(mockUpdate).toHaveBeenCalledWith("e2", {
      institution: "MIT",
      degree: "BSc",
      fieldOfStudy: null,
      startYear: 2022,
      endYear: null,
    });
  });

  it("refuses an end year before the start year", () => {
    renderEducation();
    fireEvent.click(screen.getByRole("button", { name: "Edit MIT" }));
    type("End year", "2020");
    save();
    expect(screen.getByText(educationMessages.endBeforeStart)).toBeTruthy();
    expect(mockUpdate).not.toHaveBeenCalled();
  });

  it("shows a server field error under its field", async () => {
    mockAdd.mockResolvedValue({
      ok: false,
      code: "invalid_start_year",
      message: "The start year is not valid.",
    });
    renderEducation();
    fireEvent.click(screen.getByRole("button", { name: "Add education" }));
    type("Institution", "Oxford");
    type("Start year", "2024");
    fireEvent.click(
      screen.getByRole("checkbox", { name: "I currently study here" }),
    );
    save();
    expect(
      await screen.findByText("The start year is not valid."),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("textbox", { name: "Start year" })
        .getAttribute("aria-invalid"),
    ).toBe("true");
  });

  it("deletes only after the confirm step", async () => {
    mockRemove.mockResolvedValue({ ok: true, data: undefined });
    const onSaved = renderEducation();
    fireEvent.click(screen.getByRole("button", { name: "Edit ETH Zürich" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete entry" }));
    expect(mockRemove).not.toHaveBeenCalled();
    expect(
      screen.getByText("Delete this entry? This cannot be undone."),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(
      screen.queryByText("Delete this entry? This cannot be undone."),
    ).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Delete entry" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(await screen.findByRole("button", { name: "Delete" })).toBeTruthy();
    expect(mockRemove).toHaveBeenCalledWith("e1");
    expect(onSaved).toHaveBeenCalledWith("Deleted");
  });

  it("asks Discard changes? only after a change in the form", () => {
    renderEducation();
    fireEvent.click(screen.getByRole("button", { name: "Edit MIT" }));
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByText("Discard changes?")).toBeNull();
  });
});
