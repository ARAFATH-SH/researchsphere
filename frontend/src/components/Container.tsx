import type { ElementType, ReactNode } from "react";

// Container is the only place that sets page width (docs/ui-guidelines.md, Layout widths).
// Sections never set their own max-width: a full-bleed background goes on the section and
// its content goes in a Container.

export type ContainerSize = "marketing" | "app" | "full";

const sizes: Record<ContainerSize, string> = {
  marketing: "max-w-content", // 1128px: landing and other signed-out pages
  app: "max-w-content", // 1128px: signed-in pages
  full: "", // the editor workspace
};

type ContainerProps = {
  size?: ContainerSize;
  as?: ElementType;
  className?: string;
  children?: ReactNode;
};

export function Container({ size = "app", as: Tag = "div", className = "", children }: ContainerProps) {
  const classes = ["mx-auto w-full px-gutter", sizes[size], className].filter(Boolean).join(" ");
  return <Tag className={classes}>{children}</Tag>;
}
