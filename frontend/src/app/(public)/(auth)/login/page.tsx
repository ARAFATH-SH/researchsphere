import type { Metadata } from "next";

import { AuthCard } from "@/features/auth/auth-card";
import { LoginForm } from "@/features/auth/login-form";
import { RedirectIfSignedIn } from "@/features/auth/redirect-if-signed-in";

export const metadata: Metadata = { title: "Sign in · ResearchSphere" };

export default function Page() {
  return (
    <RedirectIfSignedIn>
      <AuthCard title="Sign in" subtitle="Welcome back to ResearchSphere." joinLink>
        <LoginForm />
      </AuthCard>
    </RedirectIfSignedIn>
  );
}
