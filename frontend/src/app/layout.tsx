import type { Metadata } from "next";
import { Source_Sans_3 } from "next/font/google";

import "./globals.css";

// Source Sans 3, the design's font (design/screens/). It only sets --font-source-sans for the
// rs-font-sans token; the body keeps the system font until pages are restyled.
const sourceSans = Source_Sans_3({
  subsets: ["latin"],
  weight: ["400", "500", "700"],
  variable: "--font-source-sans",
  display: "swap",
});

export const metadata: Metadata = {
  title: "ResearchSphere",
  description: "Find collaborators, write papers together and choose where to publish.",
};

// The root layout holds only the document. The (public) and (app) route groups add the
// signed-out top bar or the signed-in shell (docs/ui-guidelines.md).
export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className={`${sourceSans.variable} h-full antialiased`}>
      <body className="flex min-h-full flex-col bg-background text-text">{children}</body>
    </html>
  );
}
