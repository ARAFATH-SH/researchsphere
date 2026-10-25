import "@/test/dialog-polyfill";

import { fireEvent, render, screen } from "@testing-library/react";

import { fakeDate } from "@/test/fake-date";

import {
  addExperience,
  type Experience,
  removeExperience,
  updateExperience,
} from "./api";
import { ExperienceDialog } from "./experience-dialog";
import { adaID, fullProfile } from "./test-profiles";
import { experienceMessages, genericError } from "./validation";

jest.mock("./api");
const mockAdd = jest.mocked(addExperience);
const mockUpdate = jest.mocked(updateExperience);
const mockRemove = jest.mocked(removeExperience);

// The year list and "not in the future" are fixed at October 2026.
beforeEach(() => fakeDate("2026-10-15T12:00:00Z"));
afterEach(() => {
  jest.useRealTimers();
  jest.resetAllMocks();
});

const [current, past] = fullProfile(adaID, "Ada Lovelace").experience;

function renderDialog(entry?: Experience) {
  const onSaved = jest.fn();
  const onDeleted = jest.fn();
  render(
    <ExperienceDialog
      entry={entry}
      onClose={() => {}}
      onSaved={onSaved}
      onDeleted={onDeleted}
    />,
  );
  return { onSaved, onDeleted };
}

const type = (name: string, value: string) =>
  fireEvent.change(screen.getByRole("textbox", { name }), {
    target: { value },
  });
const choose = (name: string, value: string) =>
  fireEvent.change(screen.getByRole("combobox", { name }), {
    target: { value },
  });
const save = () =>
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
const currentBox = () =>
  screen.getByRole("checkbox", { name: "I am currently working in this role" });

function fillRequired() {
  type("Title", "Postdoc");
  choose("Employment type", "postdoc");
  type("Organization", "ETH Zürich");
  choose("Start date: month", "2");
  choose("Start date: year", "2024");
}

describe("ExperienceDialog, adding", () => {
  it("adds a past role, trimming text and sending null for what is not set", async () => {
    const { onSaved } = renderDialog();
    expect(screen.getByRole("dialog", { name: "Add experience" })).toBeTruthy();
    expect(document.activeElement).toBe(
      screen.getByRole("textbox", { name: "Title" }),
    );
    expect(
      screen.queryByRole("button", { name: "Delete experience" }),
    ).toBeNull();
    mockAdd.mockResolvedValue({ ok: true, data: undefined });

    fillRequired();
    type("Title", "  Postdoc ");
    choose("End date: month", "6");
    choose("End date: year", "2025");
    type("Description (optional)", "  Line one\nLine two  ");
    save();

    await screen.findByRole("button", { name: "Save" });
    expect(mockAdd).toHaveBeenCalledWith({
      title: "Postdoc",
      organization: "ETH Zürich",
      employmentType: "postdoc",
      location: null,
      locationType: null,
      startDate: { year: 2024, month: 2 },
      endDate: { year: 2025, month: 6 },
      isCurrent: false,
      description: "Line one\nLine two",
    });
    expect(onSaved).toHaveBeenCalled();
  });

  it("hides and clears the end date while the role is current", async () => {
    renderDialog();
    mockAdd.mockResolvedValue({ ok: true, data: undefined });
    fillRequired();
    choose("End date: month", "6");
    choose("End date: year", "2025");

    fireEvent.click(currentBox());
    expect(
      screen.queryByRole("combobox", { name: "End date: month" }),
    ).toBeNull();
    fireEvent.click(currentBox());
    expect(
      (
        screen.getByRole("combobox", {
          name: "End date: month",
        }) as HTMLSelectElement
      ).value,
    ).toBe("");
    expect(
      (
        screen.getByRole("combobox", {
          name: "End date: year",
        }) as HTMLSelectElement
      ).value,
    ).toBe("");

    fireEvent.click(currentBox());
    save();
    await screen.findByRole("button", { name: "Save" });
    expect(mockAdd).toHaveBeenCalledWith(
      expect.objectContaining({ isCurrent: true, endDate: null }),
    );
  });

  it("checks the form before sending anything", () => {
    renderDialog();
    save();
    expect(screen.getByText(experienceMessages.titleRequired)).toBeTruthy();
    expect(
      screen.getByText(experienceMessages.employmentTypeRequired),
    ).toBeTruthy();
    expect(
      screen.getByText(experienceMessages.organizationRequired),
    ).toBeTruthy();
    expect(screen.getByText(experienceMessages.startDateRequired)).toBeTruthy();
    expect(screen.getByText(experienceMessages.endDateRequired)).toBeTruthy();
    expect(
      screen
        .getByRole("textbox", { name: "Title" })
        .getAttribute("aria-invalid"),
    ).toBe("true");
    expect(
      screen
        .getByRole("combobox", { name: "Start date: month" })
        .getAttribute("aria-describedby"),
    ).toBe("experience-start-error");
    expect(mockAdd).not.toHaveBeenCalled();
  });

  it("refuses an end before the start and a month in the future", () => {
    renderDialog();
    fillRequired();
    choose("End date: month", "1");
    choose("End date: year", "2024");
    save();
    expect(screen.getByText(experienceMessages.endBeforeStart)).toBeTruthy();

    choose("Start date: month", "11");
    choose("Start date: year", "2026");
    save();
    expect(screen.getByText(experienceMessages.startInFuture)).toBeTruthy();
    expect(mockAdd).not.toHaveBeenCalled();
  });

  it("offers years from this year back to 1950", () => {
    renderDialog();
    const years = screen.getByRole("combobox", {
      name: "Start date: year",
    }) as HTMLSelectElement;
    const values = [...years.options].map((o) => o.value).filter(Boolean);
    expect(values[0]).toBe("2026");
    expect(values.at(-1)).toBe("1950");
  });

  it("counts the description's characters", () => {
    renderDialog();
    expect(screen.getByText("0 / 2000")).toBeTruthy();
    type("Description (optional)", "Hello");
    expect(screen.getByText("5 / 2000")).toBeTruthy();
  });

  it("shows a server field error under its field", async () => {
    mockAdd.mockResolvedValue({
      ok: false,
      code: "invalid_date_range",
      message: "The end date is before the start.",
    });
    renderDialog();
    fillRequired();
    choose("End date: month", "6");
    choose("End date: year", "2025");
    save();
    expect(
      await screen.findByText("The end date is before the start."),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("combobox", { name: "End date: year" })
        .getAttribute("aria-invalid"),
    ).toBe("true");
  });

  it("explains the limit of 30 entries", async () => {
    mockAdd.mockResolvedValue({
      ok: false,
      code: "experience_limit_reached",
      message: "server text",
    });
    renderDialog();
    fillRequired();
    fireEvent.click(currentBox());
    save();
    expect((await screen.findByRole("alert")).textContent).toBe(
      "You can add up to 30 experiences.",
    );
  });

  it("shows a generic message for any other failure", async () => {
    mockAdd.mockResolvedValue({
      ok: false,
      code: "internal_error",
      message: "server text",
    });
    renderDialog();
    fillRequired();
    fireEvent.click(currentBox());
    save();
    expect((await screen.findByRole("alert")).textContent).toBe(genericError);
  });
});

describe("ExperienceDialog, editing", () => {
  it("starts with the entry's values", () => {
    renderDialog(current);
    expect(
      screen.getByRole("dialog", { name: "Edit experience" }),
    ).toBeTruthy();
    expect(
      (screen.getByRole("textbox", { name: "Title" }) as HTMLInputElement)
        .value,
    ).toBe("Research Assistant");
    expect(
      (
        screen.getByRole("combobox", {
          name: "Employment type",
        }) as HTMLSelectElement
      ).value,
    ).toBe("research_assistant");
    expect(
      (
        screen.getByRole("combobox", {
          name: "Location type (optional)",
        }) as HTMLSelectElement
      ).value,
    ).toBe("on_site");
    expect((currentBox() as HTMLInputElement).checked).toBe(true);
    expect(
      screen.queryByRole("combobox", { name: "End date: month" }),
    ).toBeNull();
  });

  it("makes a past role current with isCurrent true and endDate null", async () => {
    mockUpdate.mockResolvedValue({ ok: true, data: undefined });
    const { onSaved } = renderDialog(past);
    expect(
      (
        screen.getByRole("combobox", {
          name: "End date: year",
        }) as HTMLSelectElement
      ).value,
    ).toBe("2024");
    fireEvent.click(currentBox());
    save();
    await screen.findByRole("button", { name: "Save" });
    expect(mockUpdate).toHaveBeenCalledWith("x2", {
      title: "Teaching Assistant",
      organization: "MIT",
      employmentType: "teaching_assistant",
      location: null,
      locationType: null,
      startDate: { year: 2023, month: 1 },
      endDate: null,
      isCurrent: true,
      description: null,
    });
    expect(onSaved).toHaveBeenCalled();
  });

  it("clears an optional field with null", async () => {
    mockUpdate.mockResolvedValue({ ok: true, data: undefined });
    renderDialog(current);
    type("Location (optional)", "   ");
    choose("Location type (optional)", "");
    save();
    await screen.findByRole("button", { name: "Save" });
    expect(mockUpdate).toHaveBeenCalledWith(
      "x1",
      expect.objectContaining({ location: null, locationType: null }),
    );
  });

  it("deletes only after the confirm step inside the dialog", async () => {
    mockRemove.mockResolvedValue({ ok: true, data: undefined });
    const { onDeleted } = renderDialog(current);
    const confirmText = "Delete this experience? This cannot be undone.";
    fireEvent.click(screen.getByRole("button", { name: "Delete experience" }));
    expect(screen.getByText(confirmText)).toBeTruthy();
    expect(mockRemove).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByText(confirmText)).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Delete experience" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(await screen.findByRole("button", { name: "Delete" })).toBeTruthy();
    expect(mockRemove).toHaveBeenCalledWith("x1");
    expect(onDeleted).toHaveBeenCalled();
  });

  it("keeps the dialog open when the delete fails", async () => {
    mockRemove.mockResolvedValue({
      ok: false,
      code: "internal_error",
      message: "server text",
    });
    const { onDeleted } = renderDialog(current);
    fireEvent.click(screen.getByRole("button", { name: "Delete experience" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect((await screen.findByRole("alert")).textContent).toBe(genericError);
    expect(onDeleted).not.toHaveBeenCalled();
  });

  it("asks Discard changes? only after a change", () => {
    renderDialog(past);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByText("Discard changes?")).toBeNull();
    type("Title", "Lecturer");
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.getByText("Discard changes?")).toBeTruthy();
  });
});
