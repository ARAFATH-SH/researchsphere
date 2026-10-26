"use client";

import { useState } from "react";

import { initials } from "@/lib/initials";

// The avatar sizes: sm in the top bar, bar in the sticky profile bar and the phone top bar,
// menu in the Me menu, md on the home profile card and lg in the profile header.
const sizes = {
  sm: "size-6 text-[10px]",
  bar: "size-9 text-sm",
  menu: "size-12 text-base",
  md: "size-16 text-xl",
  lg: "size-24 text-3xl md:size-32 md:text-4xl",
};

export type AvatarSize = keyof typeof sizes;

type AvatarProps = {
  name: string;
  // src is the photo, /api/v1/media/{fileId}, or null when the user has none.
  src: string | null;
  size: AvatarSize;
  // decorative hides the avatar from screen readers where the name is already written
  // beside it, so it is not read twice.
  decorative?: boolean;
  className?: string;
};

// Avatar is a user's round profile photo (FR-4.6), or their initials on a primary circle
// when there is no photo or it does not load.
export function Avatar({ name, src, size, decorative = false, className = "" }: AvatarProps) {
  // failed remembers the src that did not load, so a new photo is tried again.
  const [failed, setFailed] = useState<string | null>(null);
  const box = `shrink-0 rounded-full ${sizes[size]} ${className}`;
  const a11y = decorative ? { "aria-hidden": true as const } : {};

  if (src && failed !== src) {
    return (
      // The media URL redirects to a short-lived presigned link, which next/image would
      // fetch and cache on the server, so a plain <img> is used.
      // eslint-disable-next-line @next/next/no-img-element
      <img
        src={src}
        alt={decorative ? "" : name}
        onError={() => setFailed(src)}
        className={`${box} bg-primary-soft object-cover`}
        {...a11y}
      />
    );
  }
  return (
    <span
      role={decorative ? undefined : "img"}
      aria-label={decorative ? undefined : name}
      className={`${box} flex items-center justify-center bg-primary font-semibold text-surface`}
      {...a11y}
    >
      {initials(name)}
    </span>
  );
}
