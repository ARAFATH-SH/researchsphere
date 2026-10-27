import { render, screen } from "@testing-library/react";

import {
  CheckboxField,
  Field,
  FieldRow,
  FormError,
  inputClass,
  rsInputClass,
  rsSelectClass,
  rsTextAreaClass,
  SelectField,
  TextAreaField,
} from "./field";

describe("Field", () => {
  it.each(["default", "rs"] as const)("links the %s control to its hint and error", (look) => {
    render(<Field id="password" label="Password" look={look} hint="At least 8 characters." error="Too short." />);
    const input = screen.getByLabelText("Password");
    expect(input.getAttribute("aria-invalid")).toBe("true");
    const ids = (input.getAttribute("aria-describedby") ?? "").split(" ");
    expect(ids.map((id) => document.getElementById(id)?.textContent)).toEqual(["Too short.", "At least 8 characters."]);
  });

  it("is not invalid and describes nothing without a hint or error", () => {
    render(<Field id="email" label="Email" look="rs" />);
    const input = screen.getByLabelText("Email");
    expect(input.getAttribute("aria-invalid")).toBeNull();
    expect(input.getAttribute("aria-describedby")).toBeNull();
  });

  it("uses the design's control only when look is rs", () => {
    const { rerender } = render(<Field id="email" label="Email" />);
    expect(screen.getByLabelText("Email").className).toBe(inputClass);
    rerender(<Field id="email" label="Email" look="rs" />);
    expect(screen.getByLabelText("Email").className).toBe(rsInputClass);
  });
});

// The profile dialogs (UI.6) use the rs look of every control.
describe("rs fields", () => {
  const label = (text: string) => screen.getByText(text, { selector: "label" });

  it("puts the label above the control with the design's style", () => {
    render(<Field id="name" label="Full name" look="rs" />);
    expect(label("Full name").className.split(" ")).toEqual(
      expect.arrayContaining(["text-rs-body", "font-medium", "text-rs-text"]),
    );
    expect(label("Full name").parentElement?.className).toContain("gap-1");
  });

  it("marks a required field with a * that is not part of the accessible name", () => {
    const { rerender } = render(<Field id="name" label="Full name" look="rs" required />);
    expect(label("Full name").className).toContain("after:content-['*']");
    expect(screen.getByRole("textbox", { name: "Full name" }).hasAttribute("required")).toBe(true);
    rerender(<Field id="name" label="Full name" look="rs" />);
    expect(label("Full name").className).not.toContain("after:content");
  });

  it("mutes the label of a disabled field", () => {
    render(<Field id="end" label="End year" look="rs" disabled />);
    expect(label("End year").className).toContain("text-rs-text-muted");
    expect((screen.getByLabelText("End year") as HTMLInputElement).disabled).toBe(true);
  });

  it("shows the error in rs-danger, linked to the control", () => {
    render(
      <SelectField id="level" label="Proficiency" look="rs" required error="Choose a proficiency.">
        <option value="">Please select</option>
      </SelectField>,
    );
    const select = screen.getByRole("combobox", { name: "Proficiency" });
    expect(select.className).toBe(rsSelectClass);
    expect(select.getAttribute("aria-invalid")).toBe("true");
    const error = document.getElementById(select.getAttribute("aria-describedby") ?? "");
    expect(error?.textContent).toBe("Choose a proficiency.");
    expect(error?.className).toContain("text-rs-danger");
  });

  it("counts the characters of a text area and turns red over the limit", () => {
    const { rerender } = render(<TextAreaField id="bio" label="Bio" look="rs" counter={{ count: 12, max: 20 }} />);
    const area = screen.getByRole("textbox", { name: "Bio" });
    expect(area.className).toBe(rsTextAreaClass);
    const hint = document.getElementById(area.getAttribute("aria-describedby") ?? "");
    expect(hint?.textContent).toBe("12 / 20");
    const count = screen.getByText("12 / 20");
    expect(count.getAttribute("aria-live")).toBe("polite");
    expect(count.className).not.toContain("text-rs-danger");
    rerender(<TextAreaField id="bio" label="Bio" look="rs" counter={{ count: 21, max: 20 }} />);
    expect(screen.getByText("21 / 20").className).toContain("text-rs-danger");
  });

  it("renders a checkbox row with its label", () => {
    render(<CheckboxField label="I currently study here" checked onChange={() => {}} />);
    const box = screen.getByRole("checkbox", { name: "I currently study here" }) as HTMLInputElement;
    expect(box.checked).toBe(true);
    expect(box.closest("label")?.className.split(" ")).toEqual(
      expect.arrayContaining(["min-h-9", "gap-2.5"]),
    );
  });

  it("lays out a row of fields that wraps", () => {
    render(
      <FieldRow>
        <Field id="a" label="Start year" look="rs" />
        <Field id="b" label="End year" look="rs" />
      </FieldRow>,
    );
    const row = screen.getByLabelText("Start year").closest(".flex-wrap");
    expect(row?.className).toContain("*:flex-[1_1_200px]");
    expect(row?.children).toHaveLength(2);
  });

  it("shows a form error as the design's danger notice", () => {
    render(<FormError look="rs">Something went wrong.</FormError>);
    expect(screen.getByRole("alert").className.split(" ")).toEqual(
      expect.arrayContaining(["bg-rs-danger-soft", "border-rs-danger-line", "text-rs-danger-strong"]),
    );
  });
});
