import { fireEvent, render, screen } from "@testing-library/react";

import { Composer } from "./composer";

const box = () => screen.getByRole("textbox", { name: "Write a message" }) as HTMLTextAreaElement;
const type = (value: string) => fireEvent.change(box(), { target: { value } });

describe("Composer", () => {
  it("sends on Enter, trims and clears", () => {
    const onSend = jest.fn();
    render(<Composer onSend={onSend} />);
    type("  hello \n");
    fireEvent.keyDown(box(), { key: "Enter" });
    expect(onSend).toHaveBeenCalledWith("hello");
    expect(box().value).toBe("");
  });

  it("adds a line on Shift+Enter instead of sending", () => {
    const onSend = jest.fn();
    render(<Composer onSend={onSend} />);
    type("hello");
    const kept = fireEvent.keyDown(box(), { key: "Enter", shiftKey: true });
    expect(onSend).not.toHaveBeenCalled();
    expect(kept).toBe(true); // not prevented, so the browser adds the line
  });

  it("does not send on Enter while an input method is composing", () => {
    const onSend = jest.fn();
    render(<Composer onSend={onSend} />);
    type("こんにちは");
    fireEvent.keyDown(box(), { key: "Enter", isComposing: true });
    expect(onSend).not.toHaveBeenCalled();
    expect(box().value).toBe("こんにちは");
  });

  it("sends with the button, and not when empty", () => {
    const onSend = jest.fn();
    render(<Composer onSend={onSend} />);
    const send = screen.getByRole("button", { name: "Send" }) as HTMLButtonElement;
    expect(send.disabled).toBe(true);
    type("   ");
    fireEvent.keyDown(box(), { key: "Enter" });
    expect(onSend).not.toHaveBeenCalled();
    type("hi");
    fireEvent.click(send);
    expect(onSend).toHaveBeenCalledWith("hi");
  });

  it("shows the count from 4,500 and refuses more than 5,000 characters", () => {
    const onSend = jest.fn();
    render(<Composer onSend={onSend} />);
    type("a".repeat(4499));
    expect(screen.queryByText(/\/ 5,000/)).toBeNull();
    type("a".repeat(4500));
    expect(screen.getByText("4,500 / 5,000")).toBeTruthy();
    type("a".repeat(5000));
    fireEvent.keyDown(box(), { key: "Enter" });
    expect(onSend).toHaveBeenCalledTimes(1);
    type("a".repeat(5001));
    expect(screen.getByText(/5,001 \/ 5,000/)).toBeTruthy();
    expect((screen.getByRole("button", { name: "Send" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.keyDown(box(), { key: "Enter" });
    expect(onSend).toHaveBeenCalledTimes(1);
  });

  it("counts an emoji once, as the server does", () => {
    render(<Composer onSend={jest.fn()} />);
    type("😀".repeat(4500));
    expect(screen.getByText("4,500 / 5,000")).toBeTruthy();
  });

  it("grows with its text up to five lines", () => {
    render(<Composer onSend={jest.fn()} />);
    const scrollHeight = jest.spyOn(box(), "scrollHeight", "get").mockReturnValue(60);
    type("a\nb\nc");
    expect(box().style.height).toBe("62px");
    expect(box().style.overflowY).toBe("hidden");
    scrollHeight.mockReturnValue(400);
    type("a\nb\nc\nd\ne\nf\ng\nh");
    expect(box().style.height).toBe("118px");
    expect(box().style.overflowY).toBe("auto");
  });

  it("is labelled, and off while disabled", () => {
    render(<Composer disabled onSend={jest.fn()} />);
    expect(box().disabled).toBe(true);
  });

  it("shows a notice as an alert", () => {
    render(<Composer notice={{ tone: "warning", text: "Slow down." }} onSend={jest.fn()} />);
    expect(screen.getByRole("alert").textContent).toBe("Slow down.");
  });
});
