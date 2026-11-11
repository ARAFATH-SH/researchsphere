import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { type Connection, listMyConnections, searchMyConnections } from "@/features/social/api";

import { createGroup, listConversations } from "./api";
import { pickDelay } from "./member-picker";
import { NewGroupDialog } from "./new-group-dialog";
import { group, nafisaID, rahimID } from "./test-data";
import { ConversationsProvider } from "./use-conversations";

const mockPush = jest.fn();
jest.mock("./api");
jest.mock("@/features/social/api");
jest.mock("next/navigation", () => ({ useRouter: () => ({ push: mockPush }) }));
jest.mock("@/features/auth/session", () => ({
  useSession: () => ({ status: "signedIn", user: { id: "0b8f4c1e-0000-4000-8000-000000000001" } }),
}));

const mockFirst = jest.mocked(listMyConnections);
const mockSearch = jest.mocked(searchMyConnections);
const mockCreate = jest.mocked(createGroup);

function connection(userId: string, fullName: string, headline: string | null = "Researcher"): Connection {
  return { userId, fullName, headline, institution: null, photoUrl: null, connectedAt: "2026-11-01T09:00:00Z", connectionStatus: "connected", connectionRequestId: null };
}

const nafisa = connection(nafisaID, "Nafisa Hasan", "Polymer chemist");
const rahim = connection(rahimID, "Rahim Uddin");
const connections = (items: Connection[]) => ({ ok: true as const, data: { items, page: 1, pageSize: 20, total: items.length } });

const onClose = jest.fn();

async function renderDialog() {
  render(
    <ConversationsProvider>
      <NewGroupDialog onClose={onClose} />
    </ConversationsProvider>,
  );
  await screen.findByRole("list", { name: "Your connections" });
}

const dialog = () => screen.getByRole("dialog");
const nameBox = () => within(dialog()).getByRole("textbox", { name: /Group name/ });
const people = () => within(dialog()).getByRole("searchbox", { name: "Add people" });
const pick = (name: string) => fireEvent.click(within(screen.getByRole("list", { name: "Your connections" })).getByRole("button", { name: new RegExp(name) }));
const chips = () => within(dialog()).queryByRole("list", { name: "Chosen people" });
const create = () => act(async () => fireEvent.click(within(dialog()).getByRole("button", { name: "Create" })));

beforeEach(() => {
  jest.useFakeTimers();
  mockFirst.mockResolvedValue(connections([nafisa, rahim]));
  jest.mocked(listConversations).mockResolvedValue({ ok: true, data: { items: [], nextCursor: null } });
  Object.defineProperty(globalThis.crypto, "randomUUID", { configurable: true, value: jest.fn(() => "group-client") });
});
afterEach(() => {
  jest.useRealTimers();
  jest.resetAllMocks();
});

describe("NewGroupDialog (FR-16.6)", () => {
  it("lists my connections, with name and headline, and searches only them as I type", async () => {
    await renderDialog();
    expect(mockFirst).toHaveBeenCalledWith(1);
    expect(screen.getByText("Polymer chemist")).toBeTruthy();
    mockSearch.mockResolvedValue(connections([rahim]));
    fireEvent.change(people(), { target: { value: "ra" } });
    await act(async () => jest.advanceTimersByTime(pickDelay - 1));
    expect(mockSearch).not.toHaveBeenCalled();
    await act(async () => jest.advanceTimersByTime(1));
    expect(mockSearch).toHaveBeenCalledWith("ra");
    expect(within(screen.getByRole("list", { name: "Your connections" })).getAllByRole("button")).toHaveLength(1);
  });

  it("counts the name and shows the chosen people as removable chips", async () => {
    await renderDialog();
    fireEvent.change(nameBox(), { target: { value: "CVS paper team" } });
    expect(within(dialog()).getByText("14 / 100")).toBeTruthy();
    pick("Nafisa Hasan");
    pick("Rahim Uddin");
    expect(within(chips()!).getAllByRole("listitem").map((li) => li.textContent)).toEqual(["Nafisa Hasan", "Rahim Uddin"]);
    fireEvent.click(within(chips()!).getByRole("button", { name: "Remove Nafisa Hasan" }));
    expect(within(chips()!).getAllByRole("listitem")).toHaveLength(1);
  });

  it("allows at most 49 people", async () => {
    const many = Array.from({ length: 50 }, (_, i) => connection(`0b8f4c1e-0000-4000-8000-0000000002${String(i).padStart(2, "0")}`, `Person ${i}`));
    mockFirst.mockResolvedValue(connections(many));
    await renderDialog();
    for (let i = 0; i < 50; i++) pick(`^Person ${i} `);
    expect(within(chips()!).getAllByRole("listitem")).toHaveLength(49);
    expect(within(dialog()).getByText("You can add up to 49 people.")).toBeTruthy();
    const last = within(screen.getByRole("list", { name: "Your connections" })).getByRole("button", { name: /^Person 49 / }) as HTMLButtonElement;
    expect(last.disabled).toBe(true);
  });

  it("creates the group with a client ID kept across tries, then opens it", async () => {
    await renderDialog();
    fireEvent.change(nameBox(), { target: { value: "  Lab  " } });
    pick("Nafisa Hasan");
    mockCreate.mockResolvedValueOnce({ ok: false, code: "network_error", message: "Could not reach the server." });
    await create();
    expect(within(dialog()).getByRole("alert").textContent).toBe("Could not reach the server.");
    mockCreate.mockResolvedValueOnce({ ok: true, data: group() });
    await create();
    expect(mockCreate.mock.calls).toEqual([
      ["Lab", [nafisaID], "group-client"],
      ["Lab", [nafisaID], "group-client"],
    ]);
    expect(onClose).toHaveBeenCalled();
    expect(mockPush).toHaveBeenCalledWith(`/messaging/${group().id}`);
  });

  it("asks for a name and at least one person before sending", async () => {
    await renderDialog();
    await create();
    expect(within(dialog()).getByText("Give the group a name.")).toBeTruthy();
    fireEvent.change(nameBox(), { target: { value: "Lab" } });
    await create();
    expect(within(dialog()).getByRole("alert").textContent).toBe("Add at least one person.");
    expect(mockCreate).not.toHaveBeenCalled();
  });

  it("marks the people the server refused, and creates once they are removed", async () => {
    await renderDialog();
    fireEvent.change(nameBox(), { target: { value: "Lab" } });
    pick("Nafisa Hasan");
    pick("Rahim Uddin");
    mockCreate.mockResolvedValueOnce({ ok: false, code: "ineligible_members", message: "No.", userIds: [rahimID] });
    await create();
    const refused = within(chips()!).getAllByRole("listitem")[1];
    expect(refused.textContent).toContain("Can't be added");
    expect(within(chips()!).getAllByRole("listitem")[0].textContent).not.toContain("Can't be added");
    expect((within(dialog()).getByRole("button", { name: "Create" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(within(refused).getByRole("button", { name: "Remove Rahim Uddin" }));
    expect((within(dialog()).getByRole("button", { name: "Create" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it.each([
    [{ ok: false as const, code: "group_full", message: "Full." }, "A group can have at most 50 members."],
    [{ ok: false as const, code: "rate_limited", message: "Slow.", retryAfter: 120 }, "You've created the most groups allowed for now. Try again in 2 minutes."],
  ])("says why it was refused: %o", async (refusal, text) => {
    await renderDialog();
    fireEvent.change(nameBox(), { target: { value: "Lab" } });
    pick("Nafisa Hasan");
    mockCreate.mockResolvedValueOnce(refusal);
    await create();
    expect(within(dialog()).getByRole("alert").textContent).toBe(text);
  });
});
