// Pill buttons from docs/ui-guidelines.md. buttonClass styles a <button> or a <Link>,
// so a link that acts as a button looks the same as one.

export type ButtonVariant = "primary" | "outline" | "text" | "inverse";

const base =
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-full font-semibold transition-colors " +
  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 " +
  "disabled:cursor-not-allowed disabled:opacity-60";

const variants: Record<ButtonVariant, string> = {
  primary: "bg-primary text-surface hover:bg-primary-hover",
  outline: "border border-primary text-primary hover:bg-primary-soft",
  text: "text-text-muted hover:bg-background hover:text-text",
  // inverse is the primary action on a primary-coloured band.
  inverse: "bg-surface text-primary hover:bg-primary-soft focus-visible:ring-surface focus-visible:ring-offset-primary",
};

const sizes = {
  md: "px-4 py-1.5 text-sm",
  lg: "px-6 py-3 text-base",
};

export function buttonClass({
  variant = "primary",
  size = "lg",
  full = false,
}: { variant?: ButtonVariant; size?: keyof typeof sizes; full?: boolean } = {}): string {
  return [base, variants[variant], sizes[size], full ? "w-full" : ""].join(" ").trim();
}
