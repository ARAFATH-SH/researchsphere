import type { ReactNode } from "react";

import type { PublicContact } from "./api";
import { birthdayText, linkLabels } from "./contact-validation";
import { EditButton } from "./edit-button";
import { LinkIcon } from "./link-icon";
import { SectionCard } from "./profile-sections";

// contactCardButtonId is the id of the card's pencil, where the focus returns when the
// contact dialog it opened closes.
export const contactCardButtonId = "edit-contact-card-button";

// ContactCard is the "Contact info" card at the top of the aside (FR-4.7), as in
// design/screens/Profile.html. It shows only what the API returned: everything that is set
// for the owner, and on anyone else's profile what the owner lets logged-in users see. The
// API leaves out a hidden item and an item that is not set alike; only the email always
// exists, so only its absence is called hidden. onEdit, given on the owner's profile, adds
// a pencil that opens the contact dialog.
export function ContactCard({
  contact,
  onEdit,
}: {
  contact: PublicContact;
  onEdit?: () => void;
}) {
  const action = onEdit && (
    <EditButton
      id={contactCardButtonId}
      label="Edit contact info"
      onClick={onEdit}
    />
  );
  return (
    <SectionCard title="Contact info" action={action} aside>
      {contact.email && (
        <Item label="Email">
          <a href={`mailto:${contact.email}`} className={`break-all ${link}`}>
            {contact.email}
          </a>
        </Item>
      )}
      {contact.phone && (
        <Item label="Phone">
          <a href={`tel:${contact.phone}`} className={link}>
            {contact.phone}
          </a>
        </Item>
      )}
      {contact.birthday && (
        <Item label="Birthday">{birthdayText(contact.birthday)}</Item>
      )}
      {contact.links.length > 0 && (
        <Item label="Websites">
          <ul aria-label="Websites" className="flex flex-wrap gap-x-3 gap-y-1">
            {contact.links.map((l) => (
              <li key={l.id} className="min-w-0">
                <a
                  href={l.url}
                  target="_blank"
                  rel="noopener noreferrer nofollow"
                  className={`inline-flex items-center gap-1 ${link}`}
                >
                  <LinkIcon kind={l.kind} className="size-4" />
                  {linkLabels[l.kind]}
                </a>
              </li>
            ))}
          </ul>
        </Item>
      )}
      {!contact.email && !onEdit && (
        <p className="text-rs-caption text-rs-text-muted">
          Email is hidden by this user.
        </p>
      )}
    </SectionCard>
  );
}

const link =
  "rounded-rs-2xs text-rs-accent-strong hover:text-rs-accent-hover hover:underline focus-visible:shadow-focus focus-visible:outline-none";

function Item({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="text-rs-body text-rs-text">
      <p className="text-rs-caption text-rs-text-muted">{label}</p>
      {children}
    </div>
  );
}
