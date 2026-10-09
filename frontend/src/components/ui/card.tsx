import type { ComponentProps } from "react";

// Card is the white, bordered, 8px-radius container every content block sits in.
export function Card({ className = "", ...props }: ComponentProps<"div">) {
  return <div className={`rounded-lg border border-border bg-surface ${className}`} {...props} />;
}
