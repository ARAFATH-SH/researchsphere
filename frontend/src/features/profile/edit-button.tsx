// EditSection names a part of the own profile that has its own edit dialog. It is also
// the value of /profile?edit=…, which opens that dialog.
export type EditSection = "intro" | "about" | "interests" | "skills" | "education" | "contact" | "photo" | "cover";

export const editSections: readonly EditSection[] = [
  "intro",
  "about",
  "interests",
  "skills",
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

// editHref links to the own profile with a section's dialog open.
export function editHref(section: EditSection): string {
  return `/profile?edit=${section}`;
}

// EditButton is the pencil on a card that opens its edit dialog. The icon is its only
// content, so label names what it edits ("Edit skills").
export function EditButton({ id, label, onClick }: { id?: string; label: string; onClick: () => void }) {
  return (
    <button
      id={id}
      type="button"
      aria-label={label}
      onClick={onClick}
      className="shrink-0 rounded-full p-2 text-text-muted transition-colors hover:bg-background hover:text-text focus-visible:ring-2 focus-visible:ring-primary focus-visible:outline-none"
    >
      <PencilIcon />
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
