import "@/test/dialog-polyfill";

import { fireEvent, render, screen } from "@testing-library/react";

import { updateMyProfile } from "./api";
import { IntroDialog } from "./intro-dialog";
import { adaID, fullProfile } from "./test-profiles";
import { genericError, introMessages } from "./validation";

jest.mock("./api");
const mockUpdate = jest.mocked(updateMyProfile);
afterEach(() => jest.resetAllMocks());

function renderIntro(country: string | null = "GB") {
  const profile = { ...fullProfile(adaID, "Ada Lovelace"), country };
  const onClose = jest.fn();
  const onSaved = jest.fn();
  render(<IntroDialog profile={profile} onClose={onClose} onSaved={onSaved} />);
  return { onClose, onSaved };
}

const save = () => fireEvent.click(screen.getByRole("button", { name: "Save" }));

describe("IntroDialog", () => {
  it("refuses an empty name without sending anything", () => {
    renderIntro();
    const name = screen.getByRole("textbox", { name: "Full name" });
    fireEvent.change(name, { target: { value: "  " } });
    save();
    expect(screen.getByText(introMessages.fullNameRequired)).toBeTruthy();
    expect(name.getAttribute("aria-invalid")).toBe("true");
    expect(name.getAttribute("aria-describedby")).toContain("intro-fullName-error");
    expect(mockUpdate).not.toHaveBeenCalled();
  });

  it("refuses a country that is not on the list", () => {
    renderIntro("XK");
    expect((screen.getByRole("combobox", { name: "Country" }) as HTMLSelectElement).value).toBe("XK");
    save();
    expect(screen.getByText(introMessages.countryInvalid)).toBeTruthy();
    expect(mockUpdate).not.toHaveBeenCalled();
  });

  it("offers every country by name", () => {
    renderIntro();
    const select = screen.getByRole("combobox", { name: "Country" });
    expect(screen.getByRole("option", { name: "Germany" })).toBeTruthy();
    // "Not set" plus the 249 countries.
    expect(select.querySelectorAll("option")).toHaveLength(250);
  });

  it("sends every field, empty ones as null, and keeps the bio", async () => {
    mockUpdate.mockResolvedValue({ ok: true, data: undefined });
    const { onSaved } = renderIntro();
    fireEvent.change(screen.getByRole("textbox", { name: "Full name" }), { target: { value: " Ada King " } });
    fireEvent.change(screen.getByRole("textbox", { name: "Headline" }), { target: { value: " " } });
    fireEvent.change(screen.getByRole("combobox", { name: "Country" }), { target: { value: "DE" } });
    save();
    expect(screen.getByRole("button", { name: "Saving…" })).toHaveProperty("disabled", true);
    await screen.findByRole("button", { name: "Save" });
    expect(mockUpdate).toHaveBeenCalledWith({
      fullName: "Ada King",
      headline: null,
      bio: "First line.\nSecond line.",
      institution: "University of London",
      department: "Mathematics",
      jobTitle: "Researcher",
      country: "DE",
    });
    expect(onSaved).toHaveBeenCalled();
  });

  it("shows a server field error under its field and anything else in a banner", async () => {
    mockUpdate.mockResolvedValueOnce({ ok: false, code: "invalid_country", message: "Country is not valid." });
    const { onSaved } = renderIntro();
    save();
    expect(await screen.findByText("Country is not valid.")).toBeTruthy();
    expect(screen.getByRole("combobox", { name: "Country" }).getAttribute("aria-invalid")).toBe("true");

    mockUpdate.mockResolvedValueOnce({ ok: false, code: "internal_error", message: "Boom" });
    save();
    expect((await screen.findByRole("alert")).textContent).toBe(genericError);
    expect(screen.queryByText("Boom")).toBeNull();
    expect(onSaved).not.toHaveBeenCalled();
  });

  it("asks Discard changes? only when something changed", () => {
    const { onClose } = renderIntro();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Discard changes?")).toBeNull();

    fireEvent.change(screen.getByRole("textbox", { name: "Headline" }), { target: { value: "New" } });
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.getByText("Discard changes?")).toBeTruthy();
    expect(onClose).toHaveBeenCalledTimes(1);

    // Typing the old value back counts as unchanged.
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Headline" }), { target: { value: "Analyst of engines" } });
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});
