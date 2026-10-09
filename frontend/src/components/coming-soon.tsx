import { Card } from "@/components/ui/card";

type ComingSoonProps = {
  title: string;
  purpose: string;
  requirements: string[];
  note?: string;
};

// ComingSoon stands in for a page whose slice is not built yet. It names the SRS
// requirements the page will cover.
export function ComingSoon({ title, purpose, requirements, note }: ComingSoonProps) {
  return (
    <Card className="space-y-3 p-6">
      <h1 className="text-2xl font-semibold">{title}</h1>
      <p className="inline-block rounded-full bg-primary-soft px-3 py-1 text-sm font-semibold text-primary">
        Coming soon
      </p>
      <p>{purpose}</p>
      <p className="text-sm text-text-muted">
        Built in a later slice: {requirements.join(", ")}.{note ? ` ${note}` : null}
      </p>
    </Card>
  );
}
