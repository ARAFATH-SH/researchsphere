import { deletedAccount, eventPreview, eventSentence, firstName, nameList } from "./system-events";
import { adaID, nafisaID, rahimID } from "./test-data";

const names: Record<string, string> = { [nafisaID]: "Nafisa Hasan", [rahimID]: "Rahim Uddin", arafath: "Arafath Hossain" };
const find = (id: string) => (names[id] ? { fullName: names[id] } : undefined);

describe("eventSentence (FR-16.6)", () => {
  it.each([
    [{ type: "group_created", userIds: [nafisaID], oldName: null, newName: "Lab" }, "arafath", "Arafath Hossain created the group"],
    [{ type: "member_added", userIds: [nafisaID], oldName: null, newName: null }, "arafath", "Arafath Hossain added Nafisa Hasan"],
    [{ type: "member_added", userIds: [nafisaID, rahimID, adaID], oldName: null, newName: null }, "arafath", "Arafath Hossain added Nafisa Hasan, Rahim Uddin and you"],
    [{ type: "member_removed", userIds: [rahimID], oldName: null, newName: null }, adaID, "You removed Rahim Uddin"],
    [{ type: "member_left", userIds: [nafisaID], oldName: null, newName: null }, nafisaID, "Nafisa Hasan left"],
    [{ type: "group_renamed", userIds: [], oldName: "A", newName: "B" }, "arafath", "Arafath Hossain renamed the group from A to B"],
    [{ type: "owner_changed", userIds: [nafisaID], oldName: null, newName: null }, null, "Nafisa Hasan is now the owner"],
    [{ type: "owner_changed", userIds: [adaID], oldName: null, newName: null }, null, "You are now the owner"],
  ] as const)("%o by %s", (event, actor, want) => {
    expect(eventSentence({ ...event, userIds: [...event.userIds], actorId: actor }, actor, adaID, find)).toBe(want);
  });

  it("names a deleted account, which the page's people leave out", () => {
    const event = { type: "member_removed" as const, userIds: ["gone"], oldName: null, newName: null, actorId: "arafath" };
    expect(eventSentence(event, "arafath", adaID, find)).toBe(`Arafath Hossain removed ${deletedAccount}`);
  });
});

describe("eventPreview", () => {
  it("says a system message in a row from the key and the actor's first name", () => {
    expect(eventPreview("group_renamed", nafisaID, "Nafisa Hasan", adaID)).toBe("Nafisa renamed the group");
    expect(eventPreview("member_added", adaID, "Ada Lovelace", adaID)).toBe("You added members");
    expect(eventPreview("member_left", rahimID, "Rahim Uddin", adaID)).toBe("Rahim left");
    expect(eventPreview("owner_changed", null, null, adaID)).toBe("The group has a new owner");
    expect(eventPreview("group_created", "x", null, adaID)).toBe("Someone created the group");
  });
});

describe("names", () => {
  it("joins names as a sentence does and takes a first name", () => {
    expect(nameList(["A"])).toBe("A");
    expect(nameList(["A", "B"])).toBe("A and B");
    expect(nameList(["A", "B", "C"])).toBe("A, B and C");
    expect(firstName("  নাফিসা হাসান ")).toBe("নাফিসা");
  });
});
