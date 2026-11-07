import { rsButtonClass } from "@/components/ui/button";

type ThreadActionsProps = {
  name: string;
  available: boolean;
  blockedByMe: boolean;
  onBlock: () => void;
  onUnblock: () => void;
  onReport: () => void;
};

// ThreadActions is the pills of a thread's header, as design/screens/Messaging.html and its _m
// file draw them. Blocking someone suspended or unverified would be refused, so Block is left
// out; an existing block can still be undone.
export function ThreadActions({ name, available, blockedByMe, onBlock, onUnblock, onReport }: ThreadActionsProps) {
  return (
    <div className="flex flex-none gap-2.5">
      {blockedByMe ? (
        <button type="button" aria-label={`Unblock ${name}`} onClick={onUnblock} className={rsButtonClass({ variant: "secondary", size: "xs" })}>
          Unblock
        </button>
      ) : (
        available && (
          <button type="button" aria-label={`Block ${name}`} onClick={onBlock} className={rsButtonClass({ variant: "secondary", size: "xs" })}>
            Block
          </button>
        )
      )}
      <button type="button" aria-label={`Report ${name}`} onClick={onReport} className={rsButtonClass({ variant: "danger", size: "xs" })}>
        Report
      </button>
    </div>
  );
}
