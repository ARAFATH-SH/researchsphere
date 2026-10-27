import { rsButtonClass } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";

// SaveButton is the dialog's submit button, the design's 44px teal pill. While saving it
// shows a spinner and cannot be pressed again.
export function SaveButton({
  saving,
  label = "Save",
}: {
  saving: boolean;
  label?: string;
}) {
  return (
    <button
      type="submit"
      disabled={saving}
      className={rsButtonClass({ variant: "primary", size: "form" })}
    >
      {saving && <Spinner />}
      {saving ? "Saving…" : label}
    </button>
  );
}

type DeleteAction = {
  // label is the first button, such as "Delete experience"; question is asked before the
  // delete happens, inside the dialog.
  label: string;
  question: string;
  confirming: boolean;
  deleting: boolean;
  onAsk: () => void;
  onCancel: () => void;
  onConfirm: () => void;
};

// FormActions is the button row at the bottom of an edit dialog, right-aligned as in
// design/screens/ProfileEdit.html: Delete (when the entry exists) and Save. Delete first asks
// its question in a box above the row, with Cancel and Delete.
export function FormActions({
  saving,
  remove,
}: {
  saving: boolean;
  remove?: DeleteAction;
}) {
  return (
    <div className="space-y-3 pt-1">
      {remove?.confirming && (
        <div
          role="group"
          aria-label="Confirm delete"
          className="space-y-3 rounded-rs-md border border-rs-danger-line p-4"
        >
          <p className="font-medium text-rs-text">{remove.question}</p>
          <div className="flex flex-wrap justify-end gap-2">
            <button
              type="button"
              onClick={remove.onCancel}
              className={rsButtonClass({ variant: "secondary", size: "form" })}
            >
              Cancel
            </button>
            <button
              type="button"
              disabled={remove.deleting}
              onClick={remove.onConfirm}
              className={rsButtonClass({ variant: "danger", size: "form" })}
            >
              {remove.deleting && <Spinner />}
              {remove.deleting ? "Deleting…" : "Delete"}
            </button>
          </div>
        </div>
      )}
      <div className="flex flex-wrap justify-end gap-2">
        {remove && !remove.confirming && (
          <button
            type="button"
            onClick={remove.onAsk}
            className={rsButtonClass({ variant: "danger", size: "form" })}
          >
            {remove.label}
          </button>
        )}
        <SaveButton saving={saving} />
      </div>
    </div>
  );
}
