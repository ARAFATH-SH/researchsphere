import { homeCard } from "@/features/home/cards";

import { ProfileLayout } from "./profile-layout";

// ProfileSkeleton has the loaded page's layout, so nothing jumps when the profile
// arrives. Screen readers hear only the status line.
export function ProfileSkeleton() {
  const bar = "animate-pulse rounded-full bg-rs-neutral";
  return (
    <>
      <p role="status" className="sr-only">
        Loading profile…
      </p>
      <div aria-hidden="true">
        <ProfileLayout
          main={
            <>
              <div className={`${homeCard} overflow-hidden`}>
                <div className="h-[200px] animate-pulse bg-rs-accent-soft max-[640px]:h-[120px]" />
                <div className="flex flex-col gap-3 px-6 pb-5">
                  <div className="-mt-[76px] size-[152px] animate-pulse rounded-full border-4 border-rs-surface bg-rs-neutral max-[640px]:-mt-[52px] max-[640px]:size-[104px]" />
                  <div className={`h-7 w-1/2 ${bar}`} />
                  <div className={`h-4 w-2/3 ${bar}`} />
                  <div className={`h-4 w-1/3 ${bar}`} />
                </div>
              </div>
              {[0, 1, 2].map((i) => (
                <div key={i} className={`${homeCard} space-y-3 p-5`}>
                  <div className={`h-5 w-1/4 ${bar}`} />
                  <div className={`h-4 w-full ${bar}`} />
                  <div className={`h-4 w-3/4 ${bar}`} />
                </div>
              ))}
            </>
          }
          aside={
            <div className={`${homeCard} space-y-3 p-4`}>
              <div className={`h-5 w-1/2 ${bar}`} />
              <div className={`h-4 w-full ${bar}`} />
              <div className={`h-4 w-2/3 ${bar}`} />
            </div>
          }
        />
      </div>
    </>
  );
}
