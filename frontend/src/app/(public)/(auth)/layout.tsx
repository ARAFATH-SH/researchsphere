import { Container } from "@/components/Container";

// Sign-up and sign-in pages: their narrow card sits centered in the page Container.
export default function AuthLayout({ children }: LayoutProps<"/">) {
  return (
    <Container size="marketing" className="py-8">
      {children}
    </Container>
  );
}
