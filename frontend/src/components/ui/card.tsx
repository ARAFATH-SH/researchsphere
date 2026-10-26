import type { ComponentProps } from "react";

// Card is the white, bordered, 8px-radius container every content block sits in. On the
// white page its rs-border line and shadow-card set it apart (design/screens).
export function Card({ className = "", ...props }: ComponentProps<"div">) {
  return <div className={`rounded-lg border border-rs-border bg-rs-surface shadow-card ${className}`} {...props} />;
}
