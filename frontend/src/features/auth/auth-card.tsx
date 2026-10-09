import Link from "next/link";
import type { ReactNode } from "react";

import { Card } from "@/components/ui/card";

type AuthCardProps = {
  title: string;
  subtitle?: ReactNode;
  children?: ReactNode;
  // signInLink adds "Already on ResearchSphere? Sign in" under the content.
  signInLink?: boolean;
};

// AuthCard is the one centered card that signed-out forms sit in (docs/ui-guidelines.md, Forms).
export function AuthCard({ title, subtitle, children, signInLink = false }: AuthCardProps) {
  return (
    <div className="mx-auto w-full max-w-[400px] space-y-5">
      <Card className="space-y-5 p-6 shadow-sm sm:p-8">
        <div className="space-y-1">
          <h1 className="text-3xl font-semibold">{title}</h1>
          {subtitle ? <div className="text-text-muted">{subtitle}</div> : null}
        </div>
        {children}
      </Card>
      {signInLink ? (
        <p className="text-center">
          Already on ResearchSphere?{" "}
          <Link href="/login" className="font-semibold text-secondary hover:underline">
            Sign in
          </Link>
        </p>
      ) : null}
    </div>
  );
}
