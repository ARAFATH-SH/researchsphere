import { PublicHeader } from "@/components/public-header";
import { SiteFooter } from "@/components/site-footer";

// Signed-out pages: the top bar with "Join now" and "Sign in" (docs/ui-guidelines.md).
export default function PublicLayout({ children }: LayoutProps<"/">) {
  return (
    <>
      <PublicHeader />
      <main className="mx-auto w-full max-w-page flex-1 px-4 py-8">{children}</main>
      <SiteFooter />
    </>
  );
}
