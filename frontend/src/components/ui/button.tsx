// Pill buttons from docs/ui-guidelines.md. buttonClass styles a <button> or a <Link>,
// so a link that acts as a button looks the same as one.

export type ButtonVariant = "primary" | "outline" | "text" | "inverse" | "danger";

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
  // danger is the action that destroys data, such as deleting the account.
  danger: "bg-danger text-surface hover:bg-danger/90 focus-visible:ring-danger",
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

// rsButtonClass is the design's pill (design/screens, UI.3): rs-accent-strong, 500 weight,
// a 1px border on every variant so filled and outline pills are the same size.
export type RsButtonVariant = "primary" | "outline" | "inverse" | "secondary" | "danger";

const rsBase =
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-rs-pill border font-medium " +
  "leading-rs-body no-underline transition-colors focus-visible:shadow-focus focus-visible:outline-none " +
  "disabled:cursor-not-allowed disabled:opacity-60";

const rsVariants: Record<RsButtonVariant, string> = {
  primary: "border-rs-accent-strong bg-rs-accent-strong text-rs-surface hover:border-rs-accent-hover hover:bg-rs-accent-hover",
  outline: "border-rs-accent-strong bg-rs-surface text-rs-accent-strong hover:bg-rs-accent-soft",
  // inverse is the primary action on an rs-accent-strong band.
  inverse: "border-rs-surface bg-rs-surface text-rs-accent-strong hover:bg-rs-accent-soft",
  // secondary is a neutral action beside a primary one, such as Cancel.
  secondary: "border-rs-control bg-rs-surface text-rs-text hover:bg-rs-subtle",
  // danger is the action that destroys data, such as Delete: rs-danger outline.
  danger: "border-rs-danger bg-rs-surface text-rs-danger hover:bg-rs-danger-soft",
};

const rsSizes = {
  // The landing hero pills: 16px text, 14px by 28px padding with the border.
  lg: "px-[27px] py-[13px] text-rs-lead",
  // Form submit buttons: 48px high.
  block: "h-12 px-[17px] text-rs-lead",
  // Secondary form buttons: 44px high.
  md: "h-11 px-[17px] text-rs-body",
  // Dialog and card form buttons (Save, Cancel, Delete): 44px high, 24px side padding.
  form: "h-11 px-[23px] text-rs-body",
  // Small pills inside cards.
  sm: "px-[17px] py-[7px] text-rs-body",
};

export function rsButtonClass({
  variant = "primary",
  size = "block",
  full = false,
}: { variant?: RsButtonVariant; size?: keyof typeof rsSizes; full?: boolean } = {}): string {
  return [rsBase, rsVariants[variant], rsSizes[size], full ? "w-full" : ""].join(" ").trim();
}
