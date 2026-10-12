import type { Metadata } from "next";

import { AuthCard } from "@/features/auth/auth-card";
import { RedirectIfSignedIn } from "@/features/auth/redirect-if-signed-in";
import { RegisterForm } from "@/features/auth/register-form";

export const metadata: Metadata = { title: "Join · ResearchSphere" };

export default function Page() {
  return (
    <RedirectIfSignedIn>
      <AuthCard title="Join ResearchSphere" subtitle="Create your research profile and start collaborating." signInLink>
        <RegisterForm />
      </AuthCard>
    </RedirectIfSignedIn>
  );
}
