import Link from "next/link";

import { Container } from "@/components/Container";
import { Logo } from "@/components/logo";
import { Card } from "@/components/ui/card";
import { buttonClass } from "@/components/ui/button";

export default function NotFound() {
  return (
    <Container as="main" size="marketing" className="flex flex-1 flex-col items-center gap-8 py-12">
      <Logo />
      <Card className="w-full max-w-[400px] space-y-4 p-6 text-center">
        <h1 className="text-2xl font-semibold">Page not found</h1>
        <p className="text-text-muted">The page you asked for does not exist.</p>
        <Link href="/" className={buttonClass({ variant: "primary", full: true })}>
          Go to the start page
        </Link>
      </Card>
    </Container>
  );
}
