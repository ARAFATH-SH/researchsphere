import { fireEvent, render, screen } from "@testing-library/react";

import { SearchBox } from "./search-box";

const mockPush = jest.fn();
let mockPathname = "/home";
let mockSearch = "";
jest.mock("next/navigation", () => ({
  usePathname: () => mockPathname,
  useRouter: () => ({ push: mockPush }),
  useSearchParams: () => new URLSearchParams(mockSearch),
}));

beforeEach(() => {
  mockPush.mockReset();
  mockPathname = "/home";
  mockSearch = "";
});

function submit(text: string) {
  const input = screen.getByLabelText("Search");
  fireEvent.change(input, { target: { value: text } });
  fireEvent.submit(input.closest("form")!);
}

describe("SearchBox", () => {
  it("goes to /search with the trimmed query on Enter", () => {
    render(<SearchBox id="s" variant="top" />);
    submit("  Rahman ");
    expect(mockPush).toHaveBeenCalledWith("/search?q=Rahman");
  });

  it.each(["a", "   ", " b "])("does nothing for the short query %j", (text) => {
    render(<SearchBox id="s" variant="top" />);
    submit(text);
    expect(mockPush).not.toHaveBeenCalled();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("shows the current query on the search page", () => {
    mockPathname = "/search";
    mockSearch = "q=ada+lovelace";
    render(<SearchBox id="s" variant="phone" />);
    expect(screen.getByLabelText<HTMLInputElement>("Search").value).toBe("ada lovelace");
  });

  it("is empty on other pages", () => {
    mockSearch = "q=ada";
    render(<SearchBox id="s" variant="top" />);
    expect(screen.getByLabelText<HTMLInputElement>("Search").value).toBe("");
  });
});
