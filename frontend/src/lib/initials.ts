// initials returns up to two letters for an avatar: the first letters of the first and
// last words of the name ("Ada King Lovelace" is "AL"), or "?" for an empty name.
export function initials(fullName: string): string {
  const words = fullName.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return "?";
  const first = Array.from(words[0])[0];
  const last = words.length > 1 ? Array.from(words[words.length - 1])[0] : "";
  return (first + last).toLocaleUpperCase();
}
