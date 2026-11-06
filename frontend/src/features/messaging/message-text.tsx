import { splitLinks } from "./links";

// MessageText shows a message as plain text, keeping its line breaks; only http(s) links
// become elements. Nothing in a message is treated as HTML.
export function MessageText({ body }: { body: string }) {
  return (
    <p className="break-words whitespace-pre-wrap [overflow-wrap:anywhere]">
      {splitLinks(body).map((part, i) =>
        part.href ? (
          <a key={i} href={part.href} target="_blank" rel="noopener noreferrer nofollow" className="underline">
            {part.text}
          </a>
        ) : (
          part.text
        ),
      )}
    </p>
  );
}
