import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";

import { addTag, removeTag, suggestTags, type Tag } from "./api";
import { suggestDelay, TagEditor } from "./tag-input";

jest.mock("./api");
const mockAdd = jest.mocked(addTag);
const mockRemove = jest.mocked(removeTag);
const mockSuggest = jest.mocked(suggestTags);
beforeEach(() => mockSuggest.mockResolvedValue({ ok: true, data: [] }));
afterEach(() => {
  jest.useRealTimers();
  jest.resetAllMocks();
});

const tag = (n: number, name = `Tag ${n}`): Tag => ({ id: `t${n}`, name });

function renderEditor(
  initial: Tag[],
  kind: "interests" | "skills" = "interests",
) {
  const onChange = jest.fn();
  render(<TagEditor kind={kind} initial={initial} onChange={onChange} />);
  const input = screen.getByRole("combobox", {
    name: kind === "interests" ? "Add a research interest" : "Add a skill",
  });
  return { onChange, input };
}

function chips() {
  return within(screen.getByRole("list", { name: "Research interests" }))
    .getAllByRole("listitem")
    .map((li) => li.textContent);
}

describe("TagEditor", () => {
  it("adds the typed tag on Enter", async () => {
    mockAdd.mockResolvedValue({
      ok: true,
      data: { id: "new", name: "Robotics" },
    });
    const { input, onChange } = renderEditor([tag(1, "NLP")]);
    fireEvent.change(input, { target: { value: "  Robotics " } });
    fireEvent.keyDown(input, { key: "Enter" });
    await screen.findByText("Robotics");
    expect(mockAdd).toHaveBeenCalledWith("interests", "Robotics");
    expect(chips()).toEqual(["NLP", "Robotics"]);
    expect((input as HTMLInputElement).value).toBe("");
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(screen.getByText("2 / 20")).toBeTruthy();
  });

  it("does nothing for a tag the user already has in another letter case", () => {
    const { input, onChange } = renderEditor([tag(1, "Machine Learning")]);
    fireEvent.change(input, { target: { value: "machine   LEARNING" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mockAdd).not.toHaveBeenCalled();
    expect(onChange).not.toHaveBeenCalled();
    expect(chips()).toEqual(["Machine Learning"]);
  });

  it("disables adding at the limit", () => {
    const tags = Array.from({ length: 20 }, (_, i) => tag(i));
    const { input } = renderEditor(tags);
    expect(input).toHaveProperty("disabled", true);
    expect(screen.getByText("20 / 20")).toBeTruthy();
    expect(screen.getByText(/reached the limit of 20/)).toBeTruthy();
  });

  it("uses the skills limit of 30", () => {
    const { input } = renderEditor([tag(1)], "skills");
    expect(input).toHaveProperty("disabled", false);
    expect(screen.getByText("1 / 30")).toBeTruthy();
  });

  it("shows the server's message when the limit is reached anyway", async () => {
    mockAdd.mockResolvedValue({
      ok: false,
      code: "tag_limit_reached",
      message: "You already have 20 interests.",
    });
    const { input } = renderEditor([tag(1)]);
    fireEvent.change(input, { target: { value: "Robotics" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect((await screen.findByRole("alert")).textContent).toBe(
      "You already have 20 interests.",
    );
  });

  it("removes a tag with its x button", async () => {
    mockRemove.mockResolvedValue({ ok: true, data: undefined });
    const { onChange } = renderEditor([tag(1, "NLP"), tag(2, "Robotics")]);
    fireEvent.click(screen.getByRole("button", { name: "Remove NLP" }));
    await waitFor(() => expect(chips()).toEqual(["Robotics"]));
    expect(mockRemove).toHaveBeenCalledWith("interests", "t1");
    expect(onChange).toHaveBeenCalled();
  });

  it("suggests tags after a pause, hides owned ones, and picks with the keyboard", async () => {
    jest.useFakeTimers();
    mockSuggest.mockResolvedValue({
      ok: true,
      data: [
        tag(7, "Machine Learning"),
        tag(8, "Machine Vision"),
        tag(1, "machine translation"),
      ],
    });
    mockAdd.mockResolvedValue({ ok: true, data: tag(8, "Machine Vision") });
    const { input } = renderEditor([tag(1, "Machine Translation")]);

    fireEvent.change(input, { target: { value: "mach" } });
    fireEvent.change(input, { target: { value: "machi" } });
    await act(async () => {
      jest.advanceTimersByTime(suggestDelay - 1);
    });
    expect(mockSuggest).not.toHaveBeenCalled();
    await act(async () => {
      jest.advanceTimersByTime(1);
    });
    expect(mockSuggest).toHaveBeenCalledTimes(1);
    expect(mockSuggest).toHaveBeenCalledWith("interests", "machi");

    const list = screen.getByRole("listbox");
    expect(
      within(list)
        .getAllByRole("option")
        .map((o) => o.textContent),
    ).toEqual(["Machine Learning", "Machine Vision"]);
    expect(input.getAttribute("aria-expanded")).toBe("true");

    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(input.getAttribute("aria-activedescendant")).toBe(
      within(list).getAllByRole("option")[1].id,
    );
    fireEvent.keyDown(input, { key: "ArrowUp" });
    fireEvent.keyDown(input, { key: "ArrowDown" });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(mockAdd).toHaveBeenCalledWith("interests", "Machine Vision");
  });

  it("closes the suggestions on Escape", async () => {
    jest.useFakeTimers();
    mockSuggest.mockResolvedValue({ ok: true, data: [tag(7, "Robotics")] });
    const { input } = renderEditor([]);
    fireEvent.change(input, { target: { value: "rob" } });
    await act(async () => {
      jest.advanceTimersByTime(suggestDelay);
    });
    expect(screen.getByRole("option", { name: "Robotics" })).toBeTruthy();
    fireEvent.keyDown(input, { key: "Escape" });
    expect(screen.queryByRole("option")).toBeNull();
    expect(input.getAttribute("aria-expanded")).toBe("false");
  });

  it("adds a suggestion on click", async () => {
    jest.useFakeTimers();
    mockSuggest.mockResolvedValue({ ok: true, data: [tag(7, "Robotics")] });
    mockAdd.mockResolvedValue({ ok: true, data: tag(7, "Robotics") });
    const { input } = renderEditor([]);
    fireEvent.change(input, { target: { value: "rob" } });
    await act(async () => {
      jest.advanceTimersByTime(suggestDelay);
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("option", { name: "Robotics" }));
    });
    expect(mockAdd).toHaveBeenCalledWith("interests", "Robotics");
    expect(chips()).toEqual(["Robotics"]);
  });
});
