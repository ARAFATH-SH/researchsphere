import { render, screen } from "@testing-library/react";

import { Field, inputClass, rsInputClass } from "./field";

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
