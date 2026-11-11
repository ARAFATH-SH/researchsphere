import { act, fireEvent, render, screen } from "@testing-library/react";

import { type Attachment, ensureSession } from "./api";
import { AttachmentCard, navigation } from "./attachment-card";

jest.mock("./api");

const file = (status: Attachment["status"], overrides: Partial<Attachment> = {}): Attachment => ({
  id: "f1",
  name: "cvs-draft-v3.pdf",
  size: 2.4 * 1024 * 1024,
  contentType: "application/pdf",
  status,
  ...overrides,
});

let go: jest.SpyInstance;
beforeEach(() => {
  go = jest.spyOn(navigation, "go").mockImplementation(() => {});
});
afterEach(() => {
  jest.restoreAllMocks();
  jest.resetAllMocks();
});

describe("AttachmentCard", () => {
  it("shows the type, name and size, and never shows the file inline", () => {
    const { container } = render(<AttachmentCard attachment={file("clean", { name: "photo.png", contentType: "image/png" })} />);
    expect(screen.getByText("IMG")).toBeTruthy();
    expect(screen.getByText("photo.png")).toBeTruthy();
    expect(container.textContent).toContain("2.4 MB");
    expect(container.querySelector("img")).toBeNull();
  });

  it.each([
    ["application/pdf", "a.pdf", "PDF"],
    ["application/vnd.openxmlformats-officedocument.wordprocessingml.document", "a.docx", "DOC"],
    ["application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "a.xlsx", "XLS"],
    ["application/vnd.openxmlformats-officedocument.presentationml.presentation", "a.pptx", "PPT"],
  ])("labels %s as %s", (contentType, name, label) => {
    render(<AttachmentCard attachment={file("clean", { contentType, name })} />);
    expect(screen.getByText(label)).toBeTruthy();
  });

  it("says a pending file is being scanned, with no download", () => {
    render(<AttachmentCard attachment={file("pending")} />);
    expect(screen.getByText("Scanning for viruses…")).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("says an infected file was removed, with no download", () => {
    render(<AttachmentCard attachment={file("infected")} />);
    expect(screen.getByText("This file was removed because it failed a security check.")).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("says a failed file is not available, with no download", () => {
    render(<AttachmentCard attachment={file("failed")} />);
    expect(screen.getByText("This file couldn't be checked and isn't available.")).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("links a clean file to the API's download", () => {
    render(<AttachmentCard attachment={file("clean")} />);
    expect(screen.getByRole("link", { name: "Download cvs-draft-v3.pdf" }).getAttribute("href")).toBe("/api/v1/attachments/f1");
  });

  it("renews the session on a normal click before going to the link", async () => {
    jest.mocked(ensureSession).mockResolvedValue(true);
    render(<AttachmentCard attachment={file("clean")} />);
    const link = screen.getByRole("link", { name: "Download cvs-draft-v3.pdf" });
    let clicked = true;
    await act(async () => {
      clicked = fireEvent.click(link);
    });
    expect(clicked).toBe(false);
    expect(ensureSession).toHaveBeenCalled();
    expect(go).toHaveBeenCalledWith("/api/v1/attachments/f1");
  });

  it("does not go to the link when the session is over", async () => {
    jest.mocked(ensureSession).mockResolvedValue(false);
    render(<AttachmentCard attachment={file("clean")} />);
    await act(async () => fireEvent.click(screen.getByRole("link", { name: "Download cvs-draft-v3.pdf" })));
    expect(go).not.toHaveBeenCalled();
  });

  it("leaves a modified or middle click to the browser", async () => {
    // Record whether the card let the click through, then stop jsdom, which cannot navigate.
    const passed: boolean[] = [];
    const stop = (e: Event) => {
      passed.push(!e.defaultPrevented);
      e.preventDefault();
    };
    window.addEventListener("click", stop);
    render(<AttachmentCard attachment={file("clean")} />);
    const link = screen.getByRole("link", { name: "Download cvs-draft-v3.pdf" });
    fireEvent.click(link, { ctrlKey: true });
    fireEvent.click(link, { button: 1 });
    window.removeEventListener("click", stop);
    expect(passed).toEqual([true, true]);
    expect(ensureSession).not.toHaveBeenCalled();
  });
});
