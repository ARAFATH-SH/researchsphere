import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";

import { removeConnection } from "@/features/social/api";

import { MoreMenu } from "./more-menu";
import { bobID } from "./test-profiles";

jest.mock("@/features/social/api");

const writeText = jest.fn<Promise<void>, [string]>();
beforeAll(() => {
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
});
afterEach(() => writeText.mockReset());

function menuButton() {
  return screen.getByRole("button", { name: "More" });
}

describe("MoreMenu", () => {
  it("opens from the keyboard with the focus on the first item and closes on Escape", () => {
    render(<MoreMenu userId={bobID} />);
    expect(menuButton().getAttribute("aria-haspopup")).toBe("menu");
    expect(screen.queryByRole("menu")).toBeNull();

    fireEvent.keyDown(menuButton(), { key: "ArrowDown" });
    expect(menuButton().getAttribute("aria-expanded")).toBe("true");
    const item = screen.getByRole("menuitem", { name: "Copy profile link" });
    expect(document.activeElement).toBe(item);

    fireEvent.keyDown(item, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(menuButton());
  });

  it("closes on a click outside", () => {
    render(<MoreMenu userId={bobID} />);
    fireEvent.click(menuButton());
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("copies the /profile/<userId> link and shows a toast", async () => {
    writeText.mockResolvedValue();
    render(<MoreMenu userId={bobID} />);
    fireEvent.click(menuButton());
    await act(async () =>
      fireEvent.click(
        screen.getByRole("menuitem", { name: "Copy profile link" }),
      ),
    );

    expect(writeText).toHaveBeenCalledWith(
      `${window.location.origin}/profile/${bobID}`,
    );
    expect((await screen.findByRole("status")).textContent).toBe("Link copied");
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("says so when copying fails", async () => {
    writeText.mockRejectedValue(new Error("denied"));
    render(<MoreMenu userId={bobID} />);
    fireEvent.click(menuButton());
    fireEvent.click(
      screen.getByRole("menuitem", { name: "Copy profile link" }),
    );
    await waitFor(() =>
      expect(screen.getByRole("status").textContent).toBe(
        "Couldn't copy the link",
      ),
    );
  });

  it("offers Remove connection only when connected", () => {
    render(<MoreMenu userId={bobID} />);
    fireEvent.click(menuButton());
    expect(screen.getAllByRole("menuitem").map((i) => i.textContent)).toEqual(["Copy profile link"]);
  });

  it("asks inside the menu, keeps the connection on Cancel and says when removing fails", async () => {
    jest.mocked(removeConnection).mockResolvedValue({ ok: false, code: "network_error", message: "Offline." });
    const onRemoved = jest.fn();
    render(<MoreMenu userId={bobID} connected={{ name: "Bob Babbage", onRemoved }} />);
    fireEvent.click(menuButton());
    fireEvent.click(screen.getByRole("menuitem", { name: "Remove connection" }));
    const remove = screen.getByRole("button", { name: "Remove" });
    expect(document.activeElement).toBe(remove);

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.getByRole("menuitem", { name: "Remove connection" })).toBeTruthy();
    expect(removeConnection).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("menuitem", { name: "Remove connection" }));
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Remove" })));
    expect(removeConnection).toHaveBeenCalledWith(bobID);
    expect(screen.getByRole("alert").textContent).toBe("Couldn't remove the connection. Try again.");
    expect(onRemoved).not.toHaveBeenCalled();
    expect(screen.getByRole("menu")).toBeTruthy();
  });
});
