import type { Metadata } from "next";

import "./globals.css";

export const metadata: Metadata = {
  title: "ResearchSphere",
  description: "Find collaborators, write papers together and choose where to publish.",
};

// The root layout holds only the document. The (public) and (app) route groups add the
// signed-out top bar or the signed-in shell (docs/ui-guidelines.md).
export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className="h-full antialiased">
      <body className="flex min-h-full flex-col bg-background text-text">{children}</body>
    </html>
  );
}
