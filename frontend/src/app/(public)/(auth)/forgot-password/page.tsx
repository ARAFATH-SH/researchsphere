import type { Metadata } from "next";

import { AuthCard } from "@/features/auth/auth-card";
import { ForgotPasswordForm } from "@/features/auth/forgot-password-form";

export const metadata: Metadata = { title: "Forgot password · ResearchSphere" };

export default function Page() {
  return (
    <AuthCard title="Forgot password" subtitle="Enter your email address and we will send you a link to choose a new password." signInLink>
      <ForgotPasswordForm />
    </AuthCard>
  );
}
