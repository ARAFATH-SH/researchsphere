import "@/test/dialog-polyfill";

import { act, fireEvent, render, screen, within } from "@testing-library/react";

import { listMyConnections } from "@/features/social/api";

import { addGroupMembers, type GroupConversation, leaveGroup, removeGroupMember, renameGroup } from "./api";
import { GroupInfoPanel, nextOwner } from "./group-info-panel";
import { adaID, group, member, nafisaID, rahimID } from "./test-data";

jest.mock("./api");
jest.mock("@/features/social/api");

const onChanged = jest.fn();
const onLeft = jest.fn();
const onReport = jest.fn();

function renderPanel(conversation: GroupConversation) {
  render(<GroupInfoPanel conversation={conversation} me={adaID} onChanged={onChanged} onLeft={onLeft} onReport={onReport} onClose={jest.fn()} />);
}

const panel = () => screen.getByRole("dialog", { name: "Group info" });
const button = (name: string | RegExp) => within(panel()).getByRole("button", { name });
const query = (name: string | RegExp) => within(panel()).queryByRole("button", { name });

beforeEach(() => {
  jest.mocked(listMyConnections).mockResolvedValue({
    ok: true,
    data: {
      items: [
        { userId: nafisaID, fullName: "Nafisa Hasan", headline: null, institution: null, photoUrl: null, connectedAt: "", connectionStatus: "connected", connectionRequestId: null },
        { userId: "0b8f4c1e-0000-4000-8000-000000000005", fullName: "Sadia Rahman", headline: null, institution: null, photoUrl: null, connectedAt: "", connectionStatus: "connected", connectionRequestId: null },
      ],
      page: 1,
      pageSize: 20,
      total: 2,
    },
  });
});
afterEach(() => jest.resetAllMocks());

describe("GroupInfoPanel (FR-16.6)", () => {
  it("lists the members with their roles and the Owner badge", () => {
    renderPanel(group());
    const rows = within(within(panel()).getByRole("list", { name: "Members" })).getAllByRole("listitem");
    expect(rows.map((r) => r.textContent)).toEqual([
      expect.stringContaining("Ada Lovelace (you)Owner"),
      expect.stringContaining("Nafisa HasanMember"),
      expect.stringContaining("Rahim UddinMember"),
    ]);
    expect(within(panel()).getByText("3 / 50 members")).toBeTruthy();
  });

  it("gives the owner Rename, Add members and Remove, and a member none of them", () => {
    renderPanel(group());
    expect(button("Rename")).toBeTruthy();
    expect(button("+ Add members")).toBeTruthy();
    expect(button("Remove Nafisa Hasan")).toBeTruthy();
    expect(query("Remove Ada Lovelace")).toBeNull();
    expect(button("Leave group")).toBeTruthy();
    expect(button("Report group")).toBeTruthy();
  });

  it("gives a member only Leave and Report", () => {
    renderPanel(group({}, "member"));
    expect(query("Rename")).toBeNull();
    expect(query("+ Add members")).toBeNull();
    expect(query(/^Remove/)).toBeNull();
    expect(button("Leave group")).toBeTruthy();
    fireEvent.click(button("Report group"));
    expect(onReport).toHaveBeenCalled();
  });

  it("renames the group and stores the answer", async () => {
    const renamed = group({}, "owner");
    renamed.group.name = "Lab 2";
    jest.mocked(renameGroup).mockResolvedValue({ ok: true, data: renamed });
    renderPanel(group());
    fireEvent.click(button("Rename"));
    fireEvent.change(within(panel()).getByRole("textbox", { name: /Group name/ }), { target: { value: " Lab 2 " } });
    await act(async () => fireEvent.click(button("Save")));
    expect(renameGroup).toHaveBeenCalledWith(group().id, "Lab 2");
    expect(onChanged).toHaveBeenCalledWith(renamed);
  });

  it("adds members with the same picker, leaving out who is already in", async () => {
    jest.mocked(addGroupMembers).mockResolvedValue({ ok: true, data: group() });
    renderPanel(group());
    fireEvent.click(button("+ Add members"));
    const list = await within(panel()).findByRole("list", { name: "Your connections" });
    expect(within(list).getAllByRole("button").map((b) => b.textContent)).toEqual([expect.stringContaining("Sadia Rahman")]);
    fireEvent.click(within(list).getByRole("button", { name: /Sadia Rahman/ }));
    await act(async () => fireEvent.click(button("Add")));
    expect(addGroupMembers).toHaveBeenCalledWith(group().id, ["0b8f4c1e-0000-4000-8000-000000000005"]);
    expect(onChanged).toHaveBeenCalled();
  });

  it("asks before removing someone, and removes them", async () => {
    jest.mocked(removeGroupMember).mockResolvedValue({ ok: true, data: undefined });
    renderPanel(group());
    fireEvent.click(button("Remove Rahim Uddin"));
    expect(within(panel()).getByRole("alertdialog", { name: "Remove Rahim Uddin?" })).toBeTruthy();
    fireEvent.click(button("Cancel"));
    expect(removeGroupMember).not.toHaveBeenCalled();
    fireEvent.click(button("Remove Rahim Uddin"));
    await act(async () => fireEvent.click(button("Remove")));
    expect(removeGroupMember).toHaveBeenCalledWith(group().id, rahimID);
    const changed = onChanged.mock.calls[0][0] as GroupConversation;
    expect(changed.group.memberCount).toBe(2);
    expect(changed.group.members?.map((m) => m.userId)).toEqual([adaID, nafisaID]);
  });

  it("tells the owner who becomes owner next before leaving: the earliest-joined member", async () => {
    jest.mocked(leaveGroup).mockResolvedValue({ ok: true, data: undefined });
    renderPanel(group());
    fireEvent.click(button("Leave group"));
    expect(within(panel()).getByRole("alertdialog").textContent).toContain("Nafisa Hasan will become the owner.");
    await act(async () => fireEvent.click(button("Leave")));
    expect(leaveGroup).toHaveBeenCalledWith(group().id);
    expect(onLeft).toHaveBeenCalled();
  });

  it("says nothing about owners when a member leaves", () => {
    renderPanel(group({}, "member"));
    fireEvent.click(button("Leave group"));
    expect(within(panel()).getByRole("alertdialog").textContent).not.toContain("will become the owner");
  });

  it("shows why a change was refused", async () => {
    jest.mocked(leaveGroup).mockResolvedValue({ ok: false, code: "network_error", message: "Could not reach the server." });
    renderPanel(group());
    fireEvent.click(button("Leave group"));
    await act(async () => fireEvent.click(button("Leave")));
    expect(within(panel()).getByRole("alert").textContent).toBe("Could not reach the server.");
    expect(onLeft).not.toHaveBeenCalled();
  });
});

describe("nextOwner", () => {
  it("is the earliest-joined other member, then the lower user ID", () => {
    const members = [
      member(adaID, "Ada", "owner", "2026-11-01T09:00:00Z"),
      member(rahimID, "Rahim", "member", "2026-11-01T10:00:00Z"),
      member(nafisaID, "Nafisa", "member", "2026-11-01T10:00:00Z"),
    ];
    expect(nextOwner(members, adaID)?.userId).toBe(nafisaID);
    expect(nextOwner([members[0]], adaID)).toBeNull();
  });
});
