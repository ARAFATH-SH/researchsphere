import { render, screen } from "@testing-library/react";

import { Highlighted, pieces } from "./highlight";

describe("pieces (FR-16.8)", () => {
  it("marks English ranges", () => {
    expect(pieces("Our paper draft", [{ start: 4, length: 5 }])).toEqual([
      { text: "Our ", marked: false },
      { text: "paper", marked: true },
      { text: " draft", marked: false },
    ]);
  });

  it("counts code points in Bangla, where a letter and its signs are separate code points", () => {
    // "আমি গবেষণা করি": গবেষণা starts at code point 4 and has 6 code points.
    expect(pieces("আমি গবেষণা করি", [{ start: 4, length: 6 }])).toEqual([
      { text: "আমি ", marked: false },
      { text: "গবেষণা", marked: true },
      { text: " করি", marked: false },
    ]);
  });

  it("never splits a character outside the BMP, which is two UTF-16 units", () => {
    expect(pieces("😀 data 😀", [{ start: 2, length: 4 }])).toEqual([
      { text: "😀 ", marked: false },
      { text: "data", marked: true },
      { text: " 😀", marked: false },
    ]);
  });

  it("clips ranges that are out of order, overlap or run past the end", () => {
    expect(pieces("abcdef", [{ start: 4, length: 9 }, { start: 0, length: 2 }, { start: 1, length: 2 }])).toEqual([
      { text: "ab", marked: true },
      { text: "c", marked: true },
      { text: "d", marked: false },
      { text: "ef", marked: true },
    ]);
  });
});

describe("Highlighted", () => {
  it("renders marks as text, never as HTML", () => {
    const { container } = render(<Highlighted snippet="<b>bold</b> paper" ranges={[{ start: 12, length: 5 }]} />);
    expect(screen.getByText("paper").tagName).toBe("MARK");
    expect(container.querySelector("b")).toBeNull();
    expect(container.textContent).toBe("<b>bold</b> paper");
  });
});
