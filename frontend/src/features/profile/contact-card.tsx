import type { ReactNode } from "react";

import type { PublicContact } from "./api";
import { birthdayText, linkLabels } from "./contact-validation";
import { EditButton } from "./edit-button";
import { SectionCard } from "./profile-sections";

// contactCardButtonId is the id of the card's pencil, where the focus returns when the
// contact dialog it opened closes.
export const contactCardButtonId = "edit-contact-card-button";

// ContactCard is the "Contact info" card at the top of the aside (FR-4.7), as in
// design/screens/Profile.html: values as plain text, websites as one line of links. It
// shows only what the API returned: everything that is set for the owner, and on anyone
// else's profile what the owner lets logged-in users see. The API leaves out a hidden item
// and an item that is not set alike; only the email always exists, so only its absence is
// called hidden. onEdit, given on the owner's profile, adds
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
          <span className="break-all">{contact.email}</span>
        </Item>
      )}
      {contact.phone && <Item label="Phone">{contact.phone}</Item>}
      {contact.birthday && (
        <Item label="Birthday">{birthdayText(contact.birthday)}</Item>
      )}
      {contact.links.length > 0 && (
        <Item label="Websites">
          <ul aria-label="Websites">
            {contact.links.map((l, i) => (
              <li key={l.id} className="inline">
                {i > 0 && " · "}
                <a
                  href={l.url}
                  target="_blank"
                  rel="noopener noreferrer nofollow"
                  className={link}
                >
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
  "rounded-rs-2xs text-rs-accent-strong underline hover:text-rs-accent-hover focus-visible:shadow-focus focus-visible:outline-none";

function Item({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="text-rs-body text-rs-text">
      <p className="text-rs-caption text-rs-text-muted">{label}</p>
      {children}
    </div>
  );
}
