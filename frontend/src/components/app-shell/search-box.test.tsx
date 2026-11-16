import { fireEvent, render, renderHook, screen } from "@testing-library/react";

import { SearchBox } from "./search-box";
import { useSearchDraftLength } from "./search-draft";

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

  it("accepts a 300-character plain-language query", () => {
    render(<SearchBox id="s" variant="top" />);
    expect(screen.getByLabelText<HTMLInputElement>("Search").maxLength).toBe(300);
    const long = "robots that help older people live alone ".repeat(8).slice(0, 300);
    submit(long);
    expect(mockPush).toHaveBeenCalledWith(`/search?${new URLSearchParams({ q: long.trim() })}`);
  });

  it("takes up to 100 characters and keeps Keyword on the search page in keyword mode", () => {
    mockPathname = "/search";
    mockSearch = "q=ada&institution=BUET&mode=keyword";
    render(<SearchBox id="s" variant="top" />);
    expect(screen.getByLabelText<HTMLInputElement>("Search").maxLength).toBe(100);
    submit("lovelace");
    expect(mockPush).toHaveBeenCalledWith("/search?q=lovelace&mode=keyword");
  });

  it("keeps the Projects tab on the search page", () => {
    mockPathname = "/search";
    mockSearch = "q=graph&type=projects&mode=keyword&page=3";
    render(<SearchBox id="s" variant="top" />);
    submit("graph models");
    expect(mockPush).toHaveBeenCalledWith("/search?q=graph+models&type=projects&mode=keyword");
  });

  it("shares the length of the text being typed", () => {
    mockPathname = "/search";
    mockSearch = "q=ada";
    const { result } = renderHook(() => useSearchDraftLength());
    render(<SearchBox id="s" variant="top" />);
    expect(result.current).toBe(3);
    fireEvent.change(screen.getByLabelText("Search"), { target: { value: "x".repeat(260) } });
    expect(result.current).toBe(260);
  });
});
