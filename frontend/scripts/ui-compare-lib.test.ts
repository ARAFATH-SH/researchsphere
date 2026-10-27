import path from "node:path";

import { designFiles, diffPercent, parseArgs, slug, viewports } from "./ui-compare-lib.mjs";

describe("parseArgs", () => {
  it.each([
    [["/login", "Auth"], { route: "/login", design: "Auth" }],
    [["--", "/login", "Auth.html"], { route: "/login", design: "Auth" }],
    [["home", "../design/screens/Home_m.html"], { route: "/home", design: "Home" }],
    [["/", "Landing"], { route: "/", design: "Landing" }],
  ])("reads %j", (argv, want) => {
    expect(parseArgs(argv)).toEqual(want);
  });

  it.each([[[]], [["/login"]], [["/login", "Auth", "extra"]], [["/login", ".html"]]])("refuses %j", (argv) => {
    expect(() => parseArgs(argv)).toThrow(/Usage/);
  });
});

describe("designFiles", () => {
  const dir = "/d";
  it("uses the _m file on the phone when it exists", () => {
    const exists = (f: string) => [path.join(dir, "Home.html"), path.join(dir, "Home_m.html")].includes(f);
    expect(designFiles(dir, "Home", exists)).toEqual({
      desktop: path.join(dir, "Home.html"),
      mobile: path.join(dir, "Home_m.html"),
    });
  });

  it("falls back to the desktop file", () => {
    const exists = (f: string) => f === path.join(dir, "Main.html");
    expect(designFiles(dir, "Main", exists).mobile).toBe(path.join(dir, "Main.html"));
  });

  it("refuses a missing design", () => {
    expect(() => designFiles(dir, "Nope", () => false)).toThrow(/No design file/);
  });
});

describe("slug", () => {
  it.each([
    ["/", "root"],
    ["/profile/abc-1", "profile-abc-1"],
    ["/search?q=x", "search"],
  ])("%s is %s", (route, want) => {
    expect(slug(route)).toBe(want);
  });
});

describe("diffPercent", () => {
  const px = (...rgba: number[][]) => Uint8Array.from(rgba.flat());

  it("is 0 for equal images", () => {
    expect(diffPercent(px([1, 2, 3, 255]), px([1, 2, 3, 255]))).toBe(0);
  });

  it("ignores changes up to the threshold and alpha", () => {
    expect(diffPercent(px([100, 100, 100, 255]), px([116, 84, 116, 0]))).toBe(0);
  });

  it("counts pixels above the threshold", () => {
    const a = px([0, 0, 0, 255], [0, 0, 0, 255], [0, 0, 0, 255], [0, 0, 0, 255]);
    const b = px([0, 0, 0, 255], [17, 0, 0, 255], [0, 0, 0, 255], [0, 0, 255, 255]);
    expect(diffPercent(a, b)).toBe(50);
  });

  it("refuses images of different sizes", () => {
    expect(() => diffPercent(px([0, 0, 0, 0]), px([0, 0, 0, 0], [0, 0, 0, 0]))).toThrow();
  });
});

it("compares at 1280x900 and 390x844", () => {
  expect(viewports.map((v) => [v.width, v.height])).toEqual([
    [1280, 900],
    [390, 844],
  ]);
});
