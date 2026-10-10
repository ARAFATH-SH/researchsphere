import { PublicHeader } from "@/components/public-header";
import { SiteFooter } from "@/components/site-footer";

// Signed-out pages: the top bar with "Join now" and "Sign in" (docs/ui-guidelines.md).
// main sets no width so landing sections can be full-bleed; each puts its content in a Container.
export default function PublicLayout({ children }: LayoutProps<"/">) {
  return (
    <>
      <PublicHeader />
      <main className="flex-1">{children}</main>
      <SiteFooter />
    </>
  );
}
