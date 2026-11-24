"use client";

import { useId, useState } from "react";

import { Dialog } from "@/components/ui/dialog";

import { barButton } from "../workspace/compile/downloads";
import { AbstractDialog } from "./abstract-dialog";
import { VenueRecommendations } from "./venue-recommendations";

// VenuesSheet is the workspace bar's "Venues" button and the side sheet it opens with the
// project's "Where to submit" card (AI-6), for members. "Add abstract" opens the abstract-only
// dialog over the sheet for the owner and editors; the card reads the venues again once it is
// saved.
export function VenuesSheet({
  projectId,
  initialAbstract,
  canEditAbstract,
}: {
  projectId: string;
  initialAbstract: string | null;
  canEditAbstract: boolean;
}) {
  const id = useId();
  const buttonId = `${id}-venues`;
  const addId = `${id}-add-abstract`;
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [abstract, setAbstract] = useState(initialAbstract);

  return (
    <>
      <button id={buttonId} type="button" aria-haspopup="dialog" onClick={() => setOpen(true)} className={barButton}>
        Venues
      </button>
      {open && (
        <Dialog title="Venues" placement="side" onClose={() => setOpen(false)} returnFocusId={buttonId}>
          <VenueRecommendations
            projectId={projectId}
            abstract={abstract}
            canEditAbstract={canEditAbstract}
            onAddAbstract={() => setEditing(true)}
            addAbstractId={addId}
            frame="plain"
          />
        </Dialog>
      )}
      {open && editing && (
        <AbstractDialog
          projectId={projectId}
          initial={abstract}
          returnFocusId={addId}
          onClose={() => setEditing(false)}
          onSaved={(next) => {
            setAbstract(next.abstract);
            setEditing(false);
          }}
        />
      )}
    </>
  );
}
