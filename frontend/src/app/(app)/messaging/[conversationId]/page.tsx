import { Thread } from "@/features/messaging/thread";

export default async function Page({ params }: PageProps<"/messaging/[conversationId]">) {
  const { conversationId } = await params;
  return <Thread key={conversationId} conversationId={conversationId} />;
}
