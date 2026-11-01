"use client";

import { useEffect } from "react";

import { SiteFooter } from "@/components/site-footer";
import { useSession } from "@/features/auth/session";
import { homeCard } from "@/features/home/cards";
import { Recommendations } from "@/features/home/recommendations";

import { type Connection, type ConnectionRequest, listMyConnections, listReceivedRequests, listSentRequests } from "./api";
import {
  ConnectionsCard,
  connectionsHeadingId,
  InvitationsCard,
  invitationsHeadingId,
  SentCard,
  sentHeadingId,
} from "./network-cards";
import { type PagedList, usePagedList } from "./paged-list";
import { setPendingInvitations } from "./pending-invitations";

const requestId = (r: ConnectionRequest) => r.requestId;
const connectionId = (c: Connection) => c.userId;

// NetworkPage is My Network (design/screens/Network.html and Network_m.html): a summary
// strip with the counts, then two columns that wrap under each other when narrow. The left
// one holds the invitations received and sent and the connections (FR-7.3 to FR-7.5). The
// right one holds the home page's recommendations card, until AI recommendations (AI-2)
// replace it, and the compact footer. The received list's total also drives the My
// Network badge, so accepting or ignoring here updates it at once.
export function NetworkPage() {
  const session = useSession();
  const me = session.status === "signedIn" ? session.user.id : null;
  const key = me ?? "";
  const received = usePagedList(key, listReceivedRequests, requestId);
  const sent = usePagedList(key, listSentRequests, requestId);
  const connections = usePagedList(key, listMyConnections, connectionId);

  const receivedTotal = received.state.status === "ok" ? received.state.total : null;
  useEffect(() => {
    if (me && receivedTotal !== null) setPendingInvitations(me, receivedTotal);
  }, [me, receivedTotal]);

  return (
    <div className="flex flex-col gap-5 max-[640px]:gap-4">
      <Summary received={received} sent={sent} connections={connections} />
      <div className="flex flex-wrap items-start gap-5 max-[640px]:gap-4">
        <div className="flex min-w-0 flex-[1_1_480px] flex-col gap-4">
          <div className="flex items-center gap-2">
            <span aria-hidden="true" className="h-[22px] w-1.5 rounded-[3px] bg-rs-accent-strong" />
            <h2 className="text-rs-lead leading-rs-snug font-medium text-rs-text">Your network</h2>
          </div>
          <InvitationsCard list={received} onAccepted={connections.prepend} />
          <SentCard list={sent} />
          <ConnectionsCard list={connections} />
        </div>
        <aside aria-label="Recommendations" className="flex min-w-0 flex-[1_1_480px] flex-col gap-4">
          <Recommendations />
          <SiteFooter variant="compact" />
        </aside>
      </div>
    </div>
  );
}

// Summary is the strip across the top: the title and a count per list, each a link to its
// card. Invitations is tinted while there are any to answer.
function Summary({
  received,
  sent,
  connections,
}: {
  received: PagedList<ConnectionRequest>;
  sent: PagedList<ConnectionRequest>;
  connections: PagedList<Connection>;
}) {
  return (
    <section
      aria-label="Network summary"
      className={`${homeCard} flex flex-wrap items-center gap-3 px-[18px] py-3.5`}
    >
      <h1 className="flex-[1_1_220px] text-rs-h3 leading-rs-tight font-bold text-rs-text">My Network</h1>
      <Count href={connectionsHeadingId} count={total(connections)} label="Connections" />
      <Count href={invitationsHeadingId} count={total(received)} label="Invitations" highlight />
      <Count href={sentHeadingId} count={total(sent)} label="Sent" />
    </section>
  );
}

function total<T>(list: PagedList<T>): number | null {
  return list.state.status === "ok" ? list.state.total : null;
}

function Count({
  href,
  count,
  label,
  highlight = false,
}: {
  href: string;
  count: number | null;
  label: string;
  highlight?: boolean;
}) {
  const tinted = highlight && count !== null && count > 0;
  return (
    <a
      href={`#${href}`}
      className={`flex min-w-[110px] flex-col items-center rounded-rs-md px-3 py-1.5 text-rs-text no-underline focus-visible:shadow-focus focus-visible:outline-none ${tinted ? "bg-rs-accent-soft" : "hover:bg-rs-subtle"}`}
    >
      <strong className="text-rs-title-md font-bold text-rs-accent-strong">{count ?? "–"}</strong>
      <span className={`text-rs-caption ${tinted ? "text-rs-heading" : "text-rs-text-muted"}`}>{label}</span>
    </a>
  );
}
