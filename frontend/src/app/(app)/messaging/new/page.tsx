import { NewConversation } from "@/features/messaging/new-conversation";

// The first message to someone: /messaging/new?to=<userId>.
export default async function Page({ searchParams }: PageProps<"/messaging/new">) {
  const { to } = await searchParams;
  return <NewConversation to={typeof to === "string" ? to : null} />;
}
