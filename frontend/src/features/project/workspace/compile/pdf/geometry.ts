// Page geometry of the PDF viewer, without pdf.js, so the panel can lay pages out before pdf.js
// has loaded.

export type PageSize = { width: number; height: number };

// A page canvas holds at most this many device pixels (16.7 million: about 64 MB of RGBA) and at
// most maxCanvasSide on a side, so a large zoom on a high-density screen cannot exhaust memory.
// Past the cap the page is drawn at a lower resolution and stretched to its size.
export const maxCanvasPixels = 2 ** 24;
export const maxCanvasSide = 8192;

export type PageBox = {
  // The page's size on screen, in CSS pixels.
  cssWidth: number;
  cssHeight: number;
  // The canvas bitmap, in device pixels: cssWidth x ratio, unless capped.
  pixelWidth: number;
  pixelHeight: number;
  capped: boolean;
};

// pageBox sizes a page drawn at scale on a screen of devicePixelRatio ratio. The bitmap is whole
// device pixels and the CSS size is that bitmap divided by ratio, so one canvas pixel lands on one
// screen pixel and the browser never resamples the page (a resampled page looks blurry). When
// ratio is a simple fraction p/q (1.5 = 3/2, 1.25 = 5/4), the bitmap is a multiple of p device
// pixels, so the CSS size is whole CSS pixels: browsers keep layout sizes in fractions of a pixel
// (Chrome 1/64), which would round 892 / 1.5 = 594.67 px. pdf.js sizes its pages the same way.
// The page is then up to p - 1 device pixels smaller than scale alone would make it.
export function pageBox(size: PageSize, scale: number, ratio: number, maxPixels = maxCanvasPixels, maxSide = maxCanvasSide): PageBox {
  const w = size.width * scale;
  const h = size.height * scale;
  const out = Math.min(ratio, Math.sqrt(maxPixels / (w * h)), maxSide / w, maxSide / h);
  if (out < ratio) {
    const pixelWidth = Math.max(1, Math.floor(w * out + 1e-6));
    const pixelHeight = Math.max(1, Math.floor(h * out + 1e-6));
    return { cssWidth: w, cssHeight: h, pixelWidth, pixelHeight, capped: true };
  }
  const [p, q] = fraction(ratio);
  if (Math.abs(p / q - ratio) < 1e-9) {
    const across = Math.max(1, Math.floor((w * ratio) / p + 1e-6));
    const down = Math.max(1, Math.floor((h * ratio) / p + 1e-6));
    return { cssWidth: across * q, cssHeight: down * q, pixelWidth: across * p, pixelHeight: down * p, capped: false };
  }
  const pixelWidth = Math.max(1, Math.floor(w * ratio + 1e-6));
  const pixelHeight = Math.max(1, Math.floor(h * ratio + 1e-6));
  return { cssWidth: pixelWidth / ratio, cssHeight: pixelHeight / ratio, pixelWidth, pixelHeight, capped: false };
}

// fraction is the closest p/q to x with q at most 10 (browser zoom 90% and 110% are 9/10, 11/10).
export function fraction(x: number): [number, number] {
  let best: [number, number] = [Math.max(1, Math.round(x)), 1];
  for (let q = 2; q <= 10; q++) {
    const p = Math.round(x * q);
    if (p >= 1 && Math.abs(x - p / q) < Math.abs(x - best[0] / best[1]) - 1e-12) best = [p, q];
  }
  return best;
}
