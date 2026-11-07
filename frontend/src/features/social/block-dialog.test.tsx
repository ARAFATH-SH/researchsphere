import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen } from "@testing-library/react";

import { blockUser, unblockUser } from "./api";
import { BlockDialog } from "./block-dialog";

jest.mock("./api");
afterEach(() => jest.resetAllMocks());

describe("BlockDialog", () => {
  it("explains a block in plain words and calls nothing until confirmed", async () => {
    jest.mocked(blockUser).mockResolvedValue({ ok: true, data: undefined });
    const onDone = jest.fn();
    render(<BlockDialog action="block" userId="u" name="Bob" onClose={jest.fn()} onDone={onDone} />);
    expect(screen.getByRole("dialog", { name: "Block Bob?" }).textContent).toContain(
      "They won't be able to message you, see your profile or find you in search. Your connection will be removed. They won't be told.",
    );
    expect(blockUser).not.toHaveBeenCalled();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Block" })));
    expect(blockUser).toHaveBeenCalledWith("u");
    expect(onDone).toHaveBeenCalled();
  });

  it("says that an unblock brings nothing back", async () => {
    jest.mocked(unblockUser).mockResolvedValue({ ok: true, data: undefined });
    const onDone = jest.fn();
    render(<BlockDialog action="unblock" userId="u" name="Bob" onClose={jest.fn()} onDone={onDone} />);
    expect(screen.getByRole("dialog", { name: "Unblock Bob?" }).textContent).toContain("Your connection won't come back.");
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Unblock" })));
    expect(unblockUser).toHaveBeenCalledWith("u");
    expect(onDone).toHaveBeenCalled();
  });

  it("shows a refusal and stays open", async () => {
    jest.mocked(blockUser).mockResolvedValue({ ok: false, code: "not_found", message: "Not found." });
    const onDone = jest.fn();
    render(<BlockDialog action="block" userId="u" name="Bob" onClose={jest.fn()} onDone={onDone} />);
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Block" })));
    expect(screen.getByRole("alert").textContent).toBe("Not found.");
    expect(onDone).not.toHaveBeenCalled();
  });

  it("cancels", () => {
    const onClose = jest.fn();
    render(<BlockDialog action="block" userId="u" name="Bob" onClose={onClose} onDone={jest.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalled();
    expect(blockUser).not.toHaveBeenCalled();
  });
});
