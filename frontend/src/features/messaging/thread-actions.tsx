import { rsButtonClass } from "@/components/ui/button";

// SearchAction is the header's "Search in this conversation" (FR-16.8): a magnifier pill.
function SearchAction({ active, onSearch }: { active: boolean; onSearch: () => void }) {
  return (
    <button
      type="button"
      aria-label="Search in this conversation"
      aria-pressed={active}
      onClick={onSearch}
      className={`${rsButtonClass({ variant: "secondary", size: "xs" })} ${active ? "bg-rs-subtle" : ""}`}
    >
      <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" className="size-4">
        <circle cx="11" cy="11" r="7" />
        <path d="m20 20-3.5-3.5" />
      </svg>
    </button>
  );
}

type ThreadActionsProps = {
  name: string;
  available: boolean;
  blockedByMe: boolean;
  searching: boolean;
  onSearch: () => void;
  onBlock: () => void;
  onUnblock: () => void;
  onReport: () => void;
};

// ThreadActions is the pills of a direct thread's header, as design/screens/Messaging.html and
// its _m file draw them, after the search. Blocking someone suspended or unverified would be
// refused, so Block is left out; an existing block can still be undone.
export function ThreadActions({ name, available, blockedByMe, searching, onSearch, onBlock, onUnblock, onReport }: ThreadActionsProps) {
  return (
    <div className="flex flex-none gap-2.5">
      <SearchAction active={searching} onSearch={onSearch} />
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

type GroupActionsProps = { searching: boolean; onSearch: () => void; onInfo: () => void };

// GroupActions is a group thread's pills (FR-16.6): search, and Group info, which holds the
// members, leaving and reporting.
export function GroupActions({ searching, onSearch, onInfo }: GroupActionsProps) {
  return (
    <div className="flex flex-none gap-2.5">
      <SearchAction active={searching} onSearch={onSearch} />
      <button id="group-info" type="button" onClick={onInfo} className={rsButtonClass({ variant: "secondary", size: "xs" })}>
        Group info
      </button>
    </div>
  );
}
