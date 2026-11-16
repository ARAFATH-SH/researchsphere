import { projectMessages, serverFieldError, validateProject } from "./validation";

const valid = { title: "Graph models", description: "A study.", abstract: "", requiredSkills: ["Python"] };

describe("validateProject", () => {
  it("accepts valid values", () => {
    expect(validateProject(valid)).toEqual({});
  });

  it.each([
    ["  ab ", projectMessages.titleShort],
    ["a".repeat(151), projectMessages.titleLong],
  ])("refuses the title %j", (title, message) => {
    expect(validateProject({ ...valid, title }).title).toBe(message);
  });

  it("counts characters as the server does, after trimming", () => {
    expect(validateProject({ ...valid, title: ` ${"😀".repeat(150)} ` }).title).toBeUndefined();
    expect(validateProject({ ...valid, title: "😀".repeat(3) }).title).toBeUndefined();
  });

  it("needs a description of at most 5,000 characters", () => {
    expect(validateProject({ ...valid, description: "  " }).description).toBe(projectMessages.descriptionRequired);
    expect(validateProject({ ...valid, description: "a".repeat(5001) }).description).toBe(projectMessages.descriptionLong);
    expect(validateProject({ ...valid, description: "a".repeat(5000) }).description).toBeUndefined();
  });

  it("takes an abstract of at most 3,000 characters", () => {
    expect(validateProject({ ...valid, abstract: "a".repeat(3001) }).abstract).toBe(projectMessages.abstractLong);
    expect(validateProject({ ...valid, abstract: "a".repeat(3000) }).abstract).toBeUndefined();
  });

  it("takes at most 20 skills", () => {
    const skills = Array.from({ length: 21 }, (_, i) => `s${i}`);
    expect(validateProject({ ...valid, requiredSkills: skills }).requiredSkills).toBe(projectMessages.skillsTooMany);
    expect(validateProject({ ...valid, requiredSkills: skills.slice(1) }).requiredSkills).toBeUndefined();
  });
});

describe("serverFieldError", () => {
  it.each([
    ["title", "title"],
    ["description", "description"],
    ["abstract", "abstract"],
    ["requiredSkills", "requiredSkills"],
    ["requiredSkills[3]", "requiredSkills"],
  ])("puts %j under %s", (field, key) => {
    expect(Object.keys(serverFieldError(field) ?? {})).toEqual([key]);
  });

  it("returns null for a field the form does not have", () => {
    expect(serverFieldError("status")).toBeNull();
    expect(serverFieldError(undefined)).toBeNull();
  });
});
