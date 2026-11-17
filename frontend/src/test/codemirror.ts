// jsdom does no layout, so a Range has no getClientRects or getBoundingClientRect, which
// CodeMirror uses to measure text. Tests that render the editor import this file; it answers
// with empty rectangles, which CodeMirror treats as text that is not on screen.
if (typeof Range !== "undefined" && !Range.prototype.getClientRects) {
  const empty = { x: 0, y: 0, width: 0, height: 0, top: 0, right: 0, bottom: 0, left: 0, toJSON: () => ({}) };
  Range.prototype.getBoundingClientRect = () => empty as DOMRect;
  Range.prototype.getClientRects = () =>
    Object.assign([], { item: () => null }) as unknown as DOMRectList;
}

export {};
