"use client";

import type { ReactNode } from "react";

import { Dialog } from "@/components/ui/dialog";

import type { PublicContact } from "./api";
import { birthdayText, linkLabels } from "./contact-validation";
import { LinkIcon } from "./link-icon";

// hasVisibleContact reports whether a profile's contact object holds anything to show.
export function hasVisibleContact(c: PublicContact): boolean {
  return Boolean(c.email || c.phone || c.birthday || c.links.length > 0);
}

// ContactViewDialog shows another user's contact info: only what the API returned, which
// is what the owner lets logged-in users see (FR-4.7).
export function ContactViewDialog({ name, contact, onClose, returnFocusId }: {
  name: string;
  contact: PublicContact;
  onClose: () => void;
  returnFocusId?: string;
}) {
  return (
    <Dialog title={`Contact info for ${name}`} onClose={onClose} returnFocusId={returnFocusId}>
      <dl className="space-y-4">
        {contact.email && (
          <Item label="Email">
            <a href={`mailto:${contact.email}`} className="break-all text-secondary hover:underline">
              {contact.email}
            </a>
          </Item>
        )}
        {contact.phone && (
          <Item label="Phone">
            <a href={`tel:${contact.phone}`} className="text-secondary hover:underline">
              {contact.phone}
            </a>
          </Item>
        )}
        {contact.birthday && <Item label="Birthday">{birthdayText(contact.birthday)}</Item>}
        {contact.links.length > 0 && (
          <Item label="Links">
            <ul aria-label="Links" className="mt-1 space-y-2">
              {contact.links.map((l) => (
                <li key={l.id}>
                  <a
                    href={l.url}
                    target="_blank"
                    rel="noopener noreferrer nofollow"
                    className="inline-flex max-w-full items-center gap-2 text-secondary hover:underline"
                  >
                    <LinkIcon kind={l.kind} />
                    <span className="min-w-0">
                      <span className="block font-semibold">{linkLabels[l.kind]}</span>
                      <span className="block truncate text-sm">{l.url}</span>
                    </span>
                  </a>
                </li>
              ))}
            </ul>
          </Item>
        )}
      </dl>
    </Dialog>
  );
}

function Item({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <dt className="text-sm font-semibold text-text-muted">{label}</dt>
      <dd className="text-base text-text">{children}</dd>
    </div>
  );
}
