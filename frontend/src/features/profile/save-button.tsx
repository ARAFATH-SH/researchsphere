import { buttonClass } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";

// SaveButton is the dialog's full-width submit button. While saving it shows a spinner
// and cannot be pressed again.
export function SaveButton({ saving, label = "Save" }: { saving: boolean; label?: string }) {
  return (
    <button type="submit" disabled={saving} className={buttonClass({ variant: "primary", full: true })}>
      {saving && <Spinner />}
      {saving ? "Saving…" : label}
    </button>
  );
}
