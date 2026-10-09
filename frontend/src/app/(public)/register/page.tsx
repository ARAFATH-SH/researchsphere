import type { Metadata } from "next";

import { AuthCard } from "@/features/auth/auth-card";
import { RegisterForm } from "@/features/auth/register-form";

export const metadata: Metadata = { title: "Join · ResearchSphere" };

export default function Page() {
  return (
    <AuthCard title="Join ResearchSphere" subtitle="Create your research profile and start collaborating." signInLink>
      <RegisterForm />
    </AuthCard>
  );
}
