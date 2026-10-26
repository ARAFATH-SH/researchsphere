import type { Metadata } from "next";

import { AuthCard } from "@/features/auth/auth-card";
import { LoginForm } from "@/features/auth/login-form";

export const metadata: Metadata = { title: "Sign in · ResearchSphere" };

// A public page: it makes no session check (no /auth/me, no refresh).
export default function Page() {
  return (
    <AuthCard title="Sign in" joinLink>
      <LoginForm />
    </AuthCard>
  );
}
