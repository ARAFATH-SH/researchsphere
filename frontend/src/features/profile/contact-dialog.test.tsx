import "@/test/dialog-polyfill";

import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import { addLink, type Contact, getMyContact, type ProfileLink, removeLink, updateMyContact } from "./api";
import { ContactDialog } from "./contact-dialog";
import { contactMessages } from "./contact-validation";

jest.mock("./api");
const mockGet = jest.mocked(getMyContact);
const mockUpdate = jest.mocked(updateMyContact);
const mockAddLink = jest.mocked(addLink);
const mockRemoveLink = jest.mocked(removeLink);
afterEach(() => jest.resetAllMocks());

const link = (n: number): ProfileLink => ({ id: `l${n}`, kind: "website", url: `https://example.org/${n}` });

function contact(over: Partial<Contact> = {}): Contact {
  return {
    email: "ada@example.org",
    emailVisibility: "only_me",
    phone: null,
    phoneVisibility: "only_me",
    birthday: null,
    birthdayVisibility: "only_me",
    links: [],
    ...over,
  };
}

async function renderContact(c: Contact = contact()) {
  mockGet.mockResolvedValue({ ok: true, data: c });
  const onClose = jest.fn();
  const onSaved = jest.fn();
  const onLinksChanged = jest.fn();
  render(<ContactDialog onClose={onClose} onSaved={onSaved} onLinksChanged={onLinksChanged} />);
  await screen.findByRole("textbox", { name: "Phone number" });
  return { onClose, onSaved, onLinksChanged };
}

const select = (name: string) => screen.getByRole("combobox", { name }) as HTMLSelectElement;
const save = () => fireEvent.click(screen.getByRole("button", { name: "Save" }));

describe("ContactDialog", () => {
  it("shows the account email read-only with its visibility", async () => {
    await renderContact(contact({ emailVisibility: "logged_in" }));
    expect(screen.getByText("ada@example.org")).toBeTruthy();
    expect(screen.queryByRole("textbox", { name: /email/i })).toBeNull();
    expect(select("Who can see your email").value).toBe("logged_in");
  });

  it("refuses an invalid phone without sending anything", async () => {
    await renderContact();
    fireEvent.change(screen.getByRole("textbox", { name: "Phone number" }), { target: { value: "01712345678" } });
    save();
    expect(screen.getByText(contactMessages.phoneInvalid)).toBeTruthy();
    expect(screen.getByRole("textbox", { name: "Phone number" }).getAttribute("aria-invalid")).toBe("true");
    expect(mockUpdate).not.toHaveBeenCalled();
  });

  it("cannot pick 30 February: the day list follows the month", async () => {
    await renderContact();
    fireEvent.change(select("Day"), { target: { value: "30" } });
    fireEvent.change(select("Month"), { target: { value: "2" } });
    expect(select("Day").value).toBe("");
    // "Not set" plus 29 days.
    expect(within(select("Day")).getAllByRole("option")).toHaveLength(30);
    fireEvent.change(select("Day"), { target: { value: "29" } });
    expect(select("Day").value).toBe("29");
  });

  it("shows the server's invalid_birthday under Birthday", async () => {
    mockUpdate.mockResolvedValue({ ok: false, code: "invalid_birthday", message: "Choose a day that exists." });
    await renderContact();
    fireEvent.change(select("Month"), { target: { value: "2" } });
    fireEvent.change(select("Day"), { target: { value: "29" } });
    save();
    expect(await screen.findByText("Choose a day that exists.")).toBeTruthy();
    expect(select("Month").getAttribute("aria-invalid")).toBe("true");
  });

  it("saves the normalized phone, the birthday and the visibilities", async () => {
    mockUpdate.mockResolvedValue({ ok: true, data: undefined });
    const { onSaved } = await renderContact();
    fireEvent.change(screen.getByRole("textbox", { name: "Phone number" }), { target: { value: "+880 1712-345678" } });
    fireEvent.change(select("Who can see your phone number"), { target: { value: "logged_in" } });
    fireEvent.change(select("Month"), { target: { value: "2" } });
    fireEvent.change(select("Day"), { target: { value: "29" } });
    save();
    expect(screen.getByRole("button", { name: "Saving…" })).toHaveProperty("disabled", true);
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(mockUpdate).toHaveBeenCalledWith({
      emailVisibility: "only_me",
      phone: "+8801712345678",
      phoneVisibility: "logged_in",
      birthday: { month: 2, day: 29 },
      birthdayVisibility: "only_me",
    });
  });

  it("clears the phone and birthday with null", async () => {
    mockUpdate.mockResolvedValue({ ok: true, data: undefined });
    await renderContact(contact({ phone: "+8801712345678", birthday: { month: 5, day: 1 } }));
    fireEvent.change(screen.getByRole("textbox", { name: "Phone number" }), { target: { value: "" } });
    fireEvent.change(select("Month"), { target: { value: "" } });
    fireEvent.change(select("Day"), { target: { value: "" } });
    save();
    await waitFor(() => expect(mockUpdate).toHaveBeenCalled());
    expect(mockUpdate.mock.calls[0][0]).toMatchObject({ phone: null, birthday: null });
  });

  it("refuses an http:// link without sending it", async () => {
    await renderContact();
    fireEvent.change(screen.getByRole("textbox", { name: "URL" }), { target: { value: "http://ada.example.org" } });
    fireEvent.click(screen.getByRole("button", { name: "Add link" }));
    expect(screen.getByText(contactMessages.linkNotHttps)).toBeTruthy();
    expect(mockAddLink).not.toHaveBeenCalled();
  });

  it("adds a link with its kind and removes one", async () => {
    mockAddLink.mockResolvedValue({ ok: true, data: { id: "new", kind: "github", url: "https://github.com/ada" } });
    mockRemoveLink.mockResolvedValue({ ok: true, data: undefined });
    const { onLinksChanged } = await renderContact(contact({ links: [link(1)] }));
    expect(screen.getByText("1 / 5", { exact: false })).toBeTruthy();

    fireEvent.change(select("Kind"), { target: { value: "github" } });
    fireEvent.change(screen.getByRole("textbox", { name: "URL" }), { target: { value: " https://github.com/ada " } });
    fireEvent.click(screen.getByRole("button", { name: "Add link" }));
    expect(await screen.findByText("https://github.com/ada")).toBeTruthy();
    expect(mockAddLink).toHaveBeenCalledWith("github", "https://github.com/ada");
    expect(screen.getByText("2 / 5", { exact: false })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Remove Website link https://example.org/1" }));
    await waitFor(() => expect(screen.queryByText("https://example.org/1")).toBeNull());
    expect(mockRemoveLink).toHaveBeenCalledWith("l1");
    expect(onLinksChanged).toHaveBeenCalledTimes(2);
  });

  it("stops adding at 5 links", async () => {
    await renderContact(contact({ links: [1, 2, 3, 4, 5].map(link) }));
    expect(screen.queryByRole("button", { name: "Add link" })).toBeNull();
    expect(screen.getByText(/reached the limit of 5 links/)).toBeTruthy();
  });

  it("shows the server's message when a 6th link is refused anyway", async () => {
    mockAddLink.mockResolvedValue({ ok: false, code: "link_limit_reached", message: "You can add at most 5 links." });
    await renderContact(contact({ links: [1, 2, 3, 4].map(link) }));
    fireEvent.change(screen.getByRole("textbox", { name: "URL" }), { target: { value: "https://example.org/6" } });
    fireEvent.click(screen.getByRole("button", { name: "Add link" }));
    expect((await screen.findByRole("alert")).textContent).toBe("You can add at most 5 links.");
  });

  it("asks Discard changes? only after a change", async () => {
    const { onClose } = await renderContact();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(1);

    fireEvent.change(select("Who can see your birthday"), { target: { value: "logged_in" } });
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.getByText("Discard changes?")).toBeTruthy();
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
