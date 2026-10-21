"use client";

import { type ReactNode, useState } from "react";

// Cover is the banner across the top of a profile card (FR-4.6): the cover image cropped
// to fill it, or a primary-to-secondary gradient when there is none or it does not load.
// The image is decorative; children, such as the edit button, sit on top of it.
export function Cover({ src, className, children }: { src: string | null; className: string; children?: ReactNode }) {
  const [failed, setFailed] = useState<string | null>(null);
  return (
    <div className={`relative bg-linear-to-r from-primary to-secondary ${className}`}>
      {src && failed !== src && (
        // The media URL redirects to a short-lived presigned link, so a plain <img>.
        // eslint-disable-next-line @next/next/no-img-element
        <img
          src={src}
          alt=""
          aria-hidden="true"
          onError={() => setFailed(src)}
          className="absolute inset-0 size-full object-cover"
        />
      )}
      {children}
    </div>
  );
}
