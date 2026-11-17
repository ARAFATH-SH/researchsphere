import type { ProjectFile } from "./api";
import {
  buildTree,
  countInside,
  figureName,
  folderAncestors,
  isInside,
  nameError,
  parentPath,
  pathError,
  uniqueName,
  visibleNodes,
} from "./tree";
import { figure, folder, textFile } from "./test-files";

describe("paths", () => {
  it("splits and checks paths", () => {
    expect(parentPath("a/b/c.tex")).toBe("a/b");
    expect(parentPath("c.tex")).toBe("");
    expect(isInside("Sections/intro.tex", "sections")).toBe(true);
    expect(isInside("sections2/intro.tex", "sections")).toBe(false);
  });

  it.each([
    ["", "Enter a name."],
    ["my file.tex", "Names can’t contain spaces (LaTeX handles them badly). Use - or _ instead."],
    [".hidden", "Names can’t start with a dot."],
    ["a".repeat(101), "Names can be at most 100 characters."],
    ["ümlaut.tex", "Use only letters A to Z, digits, -, _ and dots."],
    ["ok_name-1.tex", null],
  ])("checks the name %j", (name, message) => {
    expect(nameError(name)).toBe(message);
  });

  it("refuses a figure extension for a text file and paths too deep", () => {
    expect(pathError("", "plot.PNG", "text")).toMatch(/uploaded, not created/);
    expect(pathError("", "plot.png", "binary")).toBeNull();
    expect(pathError("a/b/c/d/e/f/g", "h.tex", "text")).toBeNull();
    expect(pathError("a/b/c/d/e/f/g/h", "i.tex", "text")).toBe("Files can be at most 8 folders deep.");
  });

  it.each([
    ["WhatsApp Image 2026-11-17 at 9.10.25 PM.jpeg", "", "whatsapp-image-2026-11-17-at-9.10.25-pm.jpeg"],
    ["scan raw (1).PNG", "", "scan-raw-1.png"],
    ["Draft figure (v2).pdf", "", "draft-figure-v2.pdf"],
    ["a  --  b__c...JPG", "", "a-b__c.jpg"],
    [".hidden.png", "", "hidden.png"],
    ["-dash-.pdf", "", "dash.pdf"],
    ["ü.png", "", "u.png"],
    ["日本.png", "", "figure.png"],
    ["Screenshot", "image/png", "screenshot.png"],
    ["photo.HEIC", "image/jpeg", "photo.heic.jpg"],
  ])("makes the uploaded name %j fit the path rules", (name, type, want) => {
    const got = figureName(name, type);
    expect(got).toBe(want);
    expect(nameError(got)).toBeNull();
  });

  it("keeps a long name within 100 characters, extension included", () => {
    const got = figureName(`${"long name ".repeat(20)}.png`);
    expect(got.length).toBeLessThanOrEqual(100);
    expect(got.endsWith(".png")).toBe(true);
    expect(nameError(got)).toBeNull();
    expect(figureName("x".repeat(300) + ".pdf", "", 20)).toBe("x".repeat(16) + ".pdf");
  });

  it("numbers a name that is taken, ignoring letter case", () => {
    expect(uniqueName("plot.png", new Set(["other.png"]))).toBe("plot.png");
    expect(uniqueName("plot.png", new Set(["plot.png"]))).toBe("plot-2.png");
    expect(uniqueName("Plot.png", new Set(["plot.png", "plot-2.png"]))).toBe("Plot-3.png");
  });
});

describe("buildTree", () => {
  const items: ProjectFile[] = [
    figure("f1", "figures/plot.png"),
    folder("d1", "figures"),
    textFile("t1", "main.tex"),
    folder("d2", "sections"),
    textFile("t2", "sections/intro.tex"),
    textFile("t3", "Abstract.tex"),
  ];

  it("nests entries, folders first and names ignoring case", () => {
    const tree = buildTree(items);
    expect(tree.map((n) => n.name)).toEqual(["figures", "sections", "Abstract.tex", "main.tex"]);
    expect(tree[0].children.map((n) => [n.name, n.depth])).toEqual([["plot.png", 1]]);
  });

  it("shows the children of expanded folders only", () => {
    const tree = buildTree(items);
    expect(visibleNodes(tree, new Set(["d2"])).map((n) => n.file.path)).toEqual([
      "figures",
      "sections",
      "sections/intro.tex",
      "Abstract.tex",
      "main.tex",
    ]);
  });

  it("counts a folder's contents and finds a path's folders", () => {
    expect(countInside(items, items[1])).toBe(1);
    expect(folderAncestors(items, "sections/intro.tex")).toEqual(["d2"]);
  });
});
