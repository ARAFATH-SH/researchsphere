const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

// dateOnly is a day in local time, as the design writes it: "3 Oct 2026".
export function dateOnly(iso: string): string {
  const d = new Date(iso);
  return `${d.getDate()} ${months[d.getMonth()]} ${d.getFullYear()}`;
}

// dateAndTime is a moment in local time: "3 Oct 2026, 6:02 PM".
export function dateAndTime(iso: string): string {
  const time = new Date(iso).toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit" });
  return `${dateOnly(iso)}, ${time}`;
}
