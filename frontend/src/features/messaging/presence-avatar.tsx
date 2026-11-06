import { Avatar, type AvatarSize } from "@/components/avatar";

import { usePresence } from "./presence";

type PresenceAvatarProps = { userId: string; name: string; src: string | null; size: AvatarSize };

// PresenceAvatar is a person's avatar with the green dot of design/screens/Messaging.html while
// they are online (FR-16.4), labelled for screen readers since the avatar itself is decorative.
export function PresenceAvatar({ userId, name, src, size }: PresenceAvatarProps) {
  const online = usePresence(userId);
  return (
    <span className="relative flex-none">
      <Avatar name={name} src={src} size={size} decorative />
      {online && (
        <span role="img" aria-label="Online" className="absolute right-0 bottom-0 size-3 rounded-full border-2 border-rs-surface bg-rs-success" />
      )}
    </span>
  );
}
