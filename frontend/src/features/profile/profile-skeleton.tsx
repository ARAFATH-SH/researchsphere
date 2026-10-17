import { Card } from "@/components/ui/card";

// ProfileSkeleton has the loaded page's layout, so nothing jumps when the profile
// arrives. Screen readers hear only the status line.
export function ProfileSkeleton() {
  const bar = "animate-pulse rounded-full bg-border";
  return (
    <>
      <p role="status" className="sr-only">
        Loading profile…
      </p>
      <div aria-hidden="true" className="grid gap-6 md:grid-cols-3">
        <div className="space-y-4 md:col-span-2">
          <Card className="overflow-hidden">
            <div className="h-24 animate-pulse bg-primary-soft md:h-32" />
            <div className="space-y-3 px-4 pb-6 md:px-6">
              <div className="-mt-12 size-24 animate-pulse rounded-full border-4 border-surface bg-border md:-mt-16 md:size-32" />
              <div className={`h-7 w-1/2 ${bar}`} />
              <div className={`h-4 w-2/3 ${bar}`} />
              <div className={`h-4 w-1/3 ${bar}`} />
            </div>
          </Card>
          {[0, 1, 2].map((i) => (
            <Card key={i} className="space-y-3 p-4 md:p-6">
              <div className={`h-5 w-1/4 ${bar}`} />
              <div className={`h-4 w-full ${bar}`} />
              <div className={`h-4 w-3/4 ${bar}`} />
            </Card>
          ))}
        </div>
        <div>
          <Card className="space-y-3 p-4 md:p-6">
            <div className={`h-5 w-1/2 ${bar}`} />
            <div className={`h-2 w-full ${bar}`} />
            <div className={`h-4 w-2/3 ${bar}`} />
          </Card>
        </div>
      </div>
    </>
  );
}
