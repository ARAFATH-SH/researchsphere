import type { Metadata } from "next";

import { AuthCard } from "@/features/auth/auth-card";
import { RegisterForm } from "@/features/auth/register-form";

export const metadata: Metadata = { title: "Join · ResearchSphere" };

// A public page: it makes no session check (no /auth/me, no refresh).
export default function Page() {
  return (
    <AuthCard title="Join ResearchSphere" subtitle="Create your research profile and start collaborating." signInLink>
      <RegisterForm />
    </AuthCard>
  );
}
