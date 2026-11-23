import { fraction, maxCanvasPixels, maxCanvasSide, pageBox } from "./geometry";

const a4 = { width: 595.28, height: 841.89 };

describe("pageBox", () => {
  it.each([1, 1.25, 1.5, 1.75, 2, 2.5, 3, 0.9, 1.1])("makes the canvas exactly CSS size x devicePixelRatio %s, in whole CSS pixels", (ratio) => {
    const [p] = fraction(ratio);
    for (const scale of [0.5, 0.75, 1, 1.25, 1.5, 0.8731]) {
      const box = pageBox(a4, scale, ratio);
      expect(box.capped).toBe(false);
      expect(Number.isInteger(box.pixelWidth) && Number.isInteger(box.pixelHeight)).toBe(true);
      expect(Number.isInteger(box.cssWidth) && Number.isInteger(box.cssHeight)).toBe(true);
      expect(box.cssWidth * ratio).toBeCloseTo(box.pixelWidth, 9);
      expect(box.cssHeight * ratio).toBeCloseTo(box.pixelHeight, 9);
      // At most p - 1 device pixels smaller than the page at this scale.
      expect(a4.width * scale * ratio - box.pixelWidth).toBeGreaterThan(-1e-6);
      expect(a4.width * scale * ratio - box.pixelWidth).toBeLessThan(p);
    }
  });

  it("keeps a ratio that is no simple fraction exact in device pixels", () => {
    const box = pageBox(a4, 1, 1.0909);
    expect(box.pixelWidth).toBe(Math.floor(a4.width * 1.0909));
    expect(box.cssWidth * 1.0909).toBeCloseTo(box.pixelWidth, 9);
  });

  it.each([
    [1, [1, 1]],
    [1.5, [3, 2]],
    [1.25, [5, 4]],
    [1.75, [7, 4]],
    [2.5, [5, 2]],
    [1.375, [11, 8]],
    [0.9, [9, 10]],
    [1.1, [11, 10]],
  ])("writes %s as a fraction", (x, want) => {
    expect(fraction(x)).toEqual(want);
  });

  it("caps the bitmap of a very large zoom and keeps the page's CSS size", () => {
    const box = pageBox(a4, 3, 3);
    expect(box.capped).toBe(true);
    expect(box.pixelWidth * box.pixelHeight).toBeLessThanOrEqual(maxCanvasPixels);
    expect(box.cssWidth).toBeCloseTo(a4.width * 3);
    expect(box.cssHeight).toBeCloseTo(a4.height * 3);
  });

  it("caps a very long side", () => {
    const strip = { width: 100, height: 20_000 };
    const box = pageBox(strip, 1, 2);
    expect(box.capped).toBe(true);
    expect(box.pixelHeight).toBeLessThanOrEqual(maxCanvasSide);
  });
});
