import { fireEvent, render, screen } from "@testing-library/react";

import { deleteAccount } from "./api";
import { DeleteAccountForm } from "./delete-account-form";

jest.mock("./api");
const mockDelete = jest.mocked(deleteAccount);

const button = () => screen.getByRole("button", { name: "Delete my account" }) as HTMLButtonElement;
const typePassword = (value: string) =>
  fireEvent.change(screen.getByLabelText("Enter your password to confirm"), { target: { value } });
const confirm = () => fireEvent.click(screen.getByLabelText("I understand this cannot be undone"));

describe("DeleteAccountForm", () => {
  afterEach(() => {
    jest.resetAllMocks();
    sessionStorage.clear();
  });

  it("enables the button only when the password and the confirmation are both given", () => {
    render(<DeleteAccountForm onDeleted={jest.fn()} />);
    expect(button().disabled).toBe(true);
    typePassword("password1");
    expect(button().disabled).toBe(true);
    confirm();
    expect(button().disabled).toBe(false);
    typePassword("");
    expect(button().disabled).toBe(true);
  });

  it("does not call the API when submitted without the confirmation", () => {
    render(<DeleteAccountForm onDeleted={jest.fn()} />);
    typePassword("password1");
    fireEvent.submit(button().closest("form")!);
    expect(mockDelete).not.toHaveBeenCalled();
  });

  it("deletes the account and leaves a note for the landing page", async () => {
    mockDelete.mockResolvedValue({ ok: true, data: undefined });
    const onDeleted = jest.fn();
    render(<DeleteAccountForm onDeleted={onDeleted} />);
    typePassword("password1");
    confirm();
    fireEvent.click(button());
    await screen.findByText("Deleting…");
    expect(mockDelete).toHaveBeenCalledWith("password1");
    expect(onDeleted).toHaveBeenCalledTimes(1);
    expect(sessionStorage.getItem("researchsphere.accountDeleted")).toBe("1");
  });

  it("shows a wrong password next to the field", async () => {
    mockDelete.mockResolvedValue({ ok: false, code: "wrong_password", message: "The password is wrong." });
    const onDeleted = jest.fn();
    render(<DeleteAccountForm onDeleted={onDeleted} />);
    typePassword("nope");
    confirm();
    fireEvent.click(button());
    expect(await screen.findByText("The password is wrong.")).toBeTruthy();
    expect(screen.getByLabelText("Enter your password to confirm").getAttribute("aria-invalid")).toBe("true");
    expect(onDeleted).not.toHaveBeenCalled();
  });

  it("warns about the projects that block the deletion, with links to them", async () => {
    mockDelete.mockResolvedValue({
      ok: false,
      code: "owns_shared_project",
      message: "You own projects that have other members.",
      projects: [
        { id: "a", title: "Alpha engine" },
        { id: "b", title: "Beta survey" },
      ],
    });
    const onDeleted = jest.fn();
    render(<DeleteAccountForm onDeleted={onDeleted} />);
    typePassword("password1");
    confirm();
    fireEvent.click(button());
    expect((await screen.findByRole("alert")).textContent).toMatch(/other members/);
    const items = screen.getAllByRole("listitem").map((li) => li.textContent);
    expect(items).toEqual(["You own Alpha engine", "You own Beta survey"]);
    expect(screen.getByRole("link", { name: "Alpha engine" }).getAttribute("href")).toBe("/projects/a");
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(onDeleted).not.toHaveBeenCalled();
    expect(sessionStorage.getItem("researchsphere.accountDeleted")).toBeNull();
  });

  it("shows other refusals, such as a locked account, in the banner", async () => {
    mockDelete.mockResolvedValue({ ok: false, code: "account_locked", message: "This account is locked." });
    render(<DeleteAccountForm onDeleted={jest.fn()} />);
    typePassword("password1");
    confirm();
    fireEvent.click(button());
    expect((await screen.findByRole("alert")).textContent).toBe("This account is locked.");
    expect(button().disabled).toBe(false);
  });
});
