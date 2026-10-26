import { emptyCard } from "./cards";

// EmptyFeed stands where the feed (FR-8.2) will be until the feed slice builds it.
export function EmptyFeed() {
  return (
    <div className={`${emptyCard} p-4 text-rs-body text-rs-text`}>
      Your feed is empty for now. Posts from you and your connections will appear here.
    </div>
  );
}
