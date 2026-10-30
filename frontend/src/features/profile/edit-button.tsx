// EditSection names a part of the own profile that has its own edit dialog. It is also
// the value of /profile?edit=…, which opens that dialog.
// "experience" and "languages" open the form for a new entry; "publications" opens the
// Add publication dialog.
export type EditSection =
  | "intro"
  | "about"
  | "interests"
  | "skills"
  | "experience"
  | "languages"
  | "publications"
  | "education"
  | "contact"
  | "photo"
  | "cover";

export const editSections: readonly EditSection[] = [
  "intro",
  "about",
  "interests",
  "skills",
  "experience",
  "languages",
  "publications",
  "education",
  "contact",
  "photo",
  "cover",
];

export function isEditSection(value: string | null): value is EditSection {
  return editSections.includes(value as EditSection);
}

// editButtonId is the id of a section's pencil, where the focus returns when its dialog closes.
export function editButtonId(section: EditSection): string {
  return `edit-${section}-button`;
}

// experienceButtonId is the id of an experience entry's pencil.
export function experienceButtonId(experienceId: string): string {
  return `edit-experience-${experienceId}-button`;
}

// languageButtonId is the id of a language's pencil.
export function languageButtonId(languageId: string): string {
  return `edit-language-${languageId}-button`;
}

// editHref links to the own profile with a section's dialog open.
export function editHref(section: EditSection): string {
  return `/profile?edit=${section}`;
}

// iconButton is the round 40px button around a pencil or "+".
const iconButton =
  "shrink-0 rounded-full p-2 text-rs-text-muted transition-colors hover:bg-rs-subtle hover:text-rs-text focus-visible:shadow-focus focus-visible:outline-none";

// EditButton is the pencil on a card that opens its edit dialog. The icon is its only
// content, so label names what it edits ("Edit skills").
export function EditButton({
  id,
  label,
  onClick,
}: {
  id?: string;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      id={id}
      type="button"
      aria-label={label}
      onClick={onClick}
      className={iconButton}
    >
      <PencilIcon />
    </button>
  );
}

// AddButton is the "+" on a card that opens the form for a new entry.
export function AddButton({
  id,
  label,
  onClick,
}: {
  id?: string;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      id={id}
      type="button"
      aria-label={label}
      onClick={onClick}
      className={iconButton}
    >
      <svg
        aria-hidden="true"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth={1.75}
        strokeLinecap="round"
        className="size-6"
      >
        <path d="M12 5v14M5 12h14" />
      </svg>
    </button>
  );
}

export function PencilIcon() {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="size-6"
    >
      <path d="M4 20h4L19 9a2.83 2.83 0 0 0-4-4L4 16v4Z" />
      <path d="m13.5 6.5 4 4" />
    </svg>
  );
}

// CameraIcon is on the button that changes the cover image.
export function CameraIcon() {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="size-6"
    >
      <path d="M4 8h3l1.5-2.5h7L17 8h3v11H4Z" />
      <circle cx="12" cy="13" r="3.5" />
    </svg>
  );
}
