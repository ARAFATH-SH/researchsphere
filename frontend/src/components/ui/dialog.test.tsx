import "@/test/dialog-polyfill";

import { fireEvent, render, screen } from "@testing-library/react";

import { Dialog } from "./dialog";

function renderDialog(dirty: boolean) {
  const onClose = jest.fn();
  render(
    <>
      <button id="opener" type="button">
        Open
      </button>
      <Dialog title="Edit about" dirty={dirty} onClose={onClose} returnFocusId="opener">
        <input aria-label="Bio" />
      </Dialog>
    </>,
  );
  return onClose;
}

describe("Dialog", () => {
  it("opens as a modal named by its title and focuses the first field", () => {
    renderDialog(false);
    const dialog = screen.getByRole("dialog", { name: "Edit about" });
    expect((dialog as HTMLDialogElement).open).toBe(true);
    expect(document.activeElement).toBe(screen.getByRole("textbox", { name: "Bio" }));
  });

  it("closes at once when nothing changed", () => {
    const onClose = renderDialog(false);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Discard changes?")).toBeNull();
  });

  it("asks Discard changes? before closing with changes, on the button and on Escape", () => {
    const onClose = renderDialog(true);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.getByText("Discard changes?")).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(screen.queryByText("Discard changes?")).toBeNull();
    expect(screen.getByRole("textbox", { name: "Bio" })).toBeTruthy();

    // Escape makes the browser fire cancel on the dialog.
    fireEvent(screen.getByRole("dialog"), new Event("cancel", { cancelable: true }));
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("gives the focus back when it goes away", () => {
    const page = (open: boolean) => (
      <>
        <button id="opener" type="button">
          Open
        </button>
        {open && (
          <Dialog title="Edit about" onClose={() => {}} returnFocusId="opener">
            <input aria-label="Bio" />
          </Dialog>
        )}
      </>
    );
    const { rerender } = render(page(true));
    expect(document.activeElement).toBe(screen.getByRole("textbox", { name: "Bio" }));
    rerender(page(false));
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Open" }));
  });
});
