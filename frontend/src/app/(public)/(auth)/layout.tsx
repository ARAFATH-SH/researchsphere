import { Container } from "@/components/Container";

// Sign-up and sign-in pages: their card sits centered in the page Container, 32px below the
// top bar and 56px above the footer (16px and 32px under 640px), as in design/screens/Auth.html.
export default function AuthLayout({ children }: LayoutProps<"/">) {
  return (
    <Container size="marketing" className="pt-8 pb-14 max-[640px]:pt-4 max-[640px]:pb-8">
      {children}
    </Container>
  );
}
