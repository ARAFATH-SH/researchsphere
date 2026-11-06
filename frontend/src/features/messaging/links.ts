// A piece of a message's text: plain text, or an http(s) link.
export type TextPart = { text: string; href?: string };

const urlPattern = /https?:\/\/[^\s<>"']+/gi;
// Punctuation that ends a sentence, not a link.
const trailing = /[.,;:!?)\]}]+$/;

// splitLinks cuts a message into text and links. The parts are shown as React text and
// plain <a> elements, so nothing in a message is ever treated as HTML.
export function splitLinks(body: string): TextPart[] {
  const parts: TextPart[] = [];
  let at = 0;
  for (const match of body.matchAll(urlPattern)) {
    const url = match[0].replace(trailing, "");
    if (!isHttpUrl(url)) continue;
    if (match.index > at) parts.push({ text: body.slice(at, match.index) });
    parts.push({ text: url, href: url });
    at = match.index + url.length;
  }
  if (at < body.length) parts.push({ text: body.slice(at) });
  return parts;
}

function isHttpUrl(url: string): boolean {
  try {
    const { protocol } = new URL(url);
    return protocol === "http:" || protocol === "https:";
  } catch {
    return false;
  }
}
