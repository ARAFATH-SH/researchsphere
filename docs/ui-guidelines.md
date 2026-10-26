# UI guidelines

How ResearchSphere looks. The layout and patterns follow familiar professional and research
networks (white cards on a light page, a top bar with icon tabs), but the name, logo and
colors are our own. **Never copy another product's logo, name, wordmark, images or exact
brand colors** (for example LinkedIn's or ResearchGate's).

The tokens below are Tailwind theme tokens in `frontend/src/app/globals.css` (`@theme`).
Components use only these names, for example `bg-background`, `bg-surface`,
`border-border`, `text-text-muted`, `bg-primary`, `text-secondary`. Never write raw colors
(hex values, `white`, `gray-400` …) in components.

## Design reference

`design/screens/` is the source of truth for look and spacing: colors, fonts, sizes, radii,
shadows, gaps and widths. Each screen has a desktop artboard (`<Name>.html`, 1280px) and a
mobile one (`<Name>_m.html`, 390px); `Main.html` is a requirement map, not a screen. Use the
light theme only and ignore the design-tool markup (`{{theme}}`, `<x-dc>`, `<sc-if>`,
`support.js`); see `design/README.md`.

- When this document and the design disagree on how something looks, the design wins.
  Layout rules here (Container, grids, accessibility, what each page holds) still apply.
- Pages move to the design tokens below one at a time. A restyled page uses only `rs-*`
  tokens; the [legacy tokens](#foundations-legacy) are removed once no page uses them.
- Check a page against its design with `npm run ui:compare` ([Visual
  comparison](#visual-comparison)).

## Design tokens

CSS variables in `:root` of `frontend/src/app/globals.css`, each mapped to a Tailwind utility
in its `@theme inline` block. Taken from the 15 desktop screens; light theme only.
`src/app/tokens.test.ts` fails when a token is missing from Tailwind or from these tables.

### Colors

Tailwind: `bg-`, `text-`, `border-`, `ring-` … plus the name after `--`, for example
`bg-rs-surface`, `text-rs-heading`, `border-rs-border`.

| Variable | Value | Use |
| --- | --- | --- |
| `--rs-surface` | `#FFFFFF` | Page background, cards, top bar |
| `--rs-text` | `#191919` | Main text |
| `--rs-text-muted` | `#666666` | Secondary text, meta lines |
| `--rs-heading` | `#0B5F58` | Card and section headings |
| `--rs-accent` | `#0B7A75` | Logo, active tab, nav accent, focus ring |
| `--rs-accent-strong` | `#0F766E` | Links, primary buttons |
| `--rs-accent-hover` | `#115E59` | Hover of links and `accent-strong` |
| `--rs-accent-deep` | `#134E4A` | Deep teal fills (initials, banners) |
| `--rs-accent-soft` | `#E6F4F2` | Soft teal fills: chips, tiles, selected rows |
| `--rs-accent-line` | `#9FD3CC` | Border of soft teal boxes |
| `--rs-border` | `#DDE3E1` | Card borders |
| `--rs-divider` | `#EEF2F1` | Dividers between rows inside a card |
| `--rs-control` | `#8A979E` | Input and dashed borders |
| `--rs-subtle` | `#F5F8F7` | Subtle panels inside a card |
| `--rs-neutral` | `#E3E8E7` | Neutral fills, skeletons |
| `--rs-search` | `#EDF3F3` | Top-bar search field |
| `--rs-nav-border` | `#E2E7E7` | Top bar and footer borders |
| `--rs-nav-text` | `rgba(0,0,0,.9)` | Top-bar text, active tab |
| `--rs-nav-muted` | `rgba(0,0,0,.6)` | Inactive tabs, menu links |
| `--rs-footer-bg` | `#F4F6F6` | Footer background |
| `--rs-footer-muted` | `#5C6868` | Footer secondary text |
| `--rs-footer-link` | `#46625F` | Footer links |
| `--rs-danger` | `#B42318` | Errors, danger buttons, count badges |
| `--rs-danger-strong` | `#912018` | Error text on `danger-soft` |
| `--rs-danger-soft` | `#FEF3F2` | Error banner background |
| `--rs-danger-line` | `#F4C7C3` | Danger card border |
| `--rs-warning` | `#7A4100` | Warning text |
| `--rs-warning-soft` | `#FFF8E6` | Warning banner background |
| `--rs-warning-line` | `#F0C27B` | Warning border |
| `--rs-orange` | `#8A4B00` | Orange chip text |
| `--rs-orange-soft` | `#FFF4E5` | Orange chip background |
| `--rs-violet` | `#5B21B6` | Violet chip text |
| `--rs-violet-soft` | `#EDE9FE` | Violet chip background |
| `--rs-info` | `#2B6CB0` | Info accents |
| `--rs-success` | `#15803D` | Success text |

One-offs in the design that are not tokens (avatar and chip shades used once or twice):
`#7B341E`, `#9A3412`, `#09615D`, `#7C3AED`, `#16A34A`, `#F1F4F3`, `#0D3B38`, `#553C9A`,
`#C4CDD1`, `#C2410C`, `#F5F3FF`, `#C4B5FD`, `#4C1D95`, `rgba(11,122,117,.10)`. If a page
needs one, add a token first.

### Type

| Variable | Value | Tailwind |
| --- | --- | --- |
| `--rs-font-sans` | Source Sans 3 (400, 500, 700, from `next/font/google` in `app/layout.tsx`), then `'Helvetica Neue', Arial, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif` | `font-rs-sans` (page text) |
| `--rs-font-ui` | `-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif` | `font-rs-ui` (top bar and footer) |
| `--rs-font-mono` | `'JetBrains Mono', ui-monospace, monospace` (no web font loaded) | `font-rs-mono` |
| `--rs-size-micro` | 10px | `text-rs-micro` |
| `--rs-size-tiny` | 11px | `text-rs-tiny` |
| `--rs-size-caption` | 12px | `text-rs-caption` |
| `--rs-size-body` | 14px (the most used size) | `text-rs-body` |
| `--rs-size-lead` | 16px | `text-rs-lead` |
| `--rs-size-title-sm` | 18px | `text-rs-title-sm` |
| `--rs-size-title` | 20px | `text-rs-title` |
| `--rs-size-title-md` | 22px | `text-rs-title-md` |
| `--rs-size-h3` | 24px | `text-rs-h3` |
| `--rs-size-h2` | 28px | `text-rs-h2` |
| `--rs-size-h1` | 32px | `text-rs-h1` |
| `--rs-size-display` | 52px (landing headline) | `text-rs-display` |
| `--rs-leading-tight` | 1.2 | `leading-rs-tight` |
| `--rs-leading-snug` | 1.3 (the most used) | `leading-rs-snug` |
| `--rs-leading-body` | 1.45 | `leading-rs-body` |
| `--rs-leading-relaxed` | 1.5 | `leading-rs-relaxed` |

Weights are 400, 500 and 700: Tailwind's `font-normal`, `font-medium`, `font-bold`.

### Radii, shadows and widths

| Variable | Value | Tailwind |
| --- | --- | --- |
| `--rs-radius-pill` | 999px (buttons, chips) | `rounded-rs-pill` |
| `--rs-radius-card` | 10px | `rounded-card` |
| `--rs-radius-card-lg` | 12px | `rounded-card-lg` |
| `--rs-radius-md` | 8px (menus, inputs) | `rounded-rs-md` |
| `--rs-radius-control` | 6px | `rounded-control` |
| `--rs-radius-chip` | 5px | `rounded-rs-chip` |
| `--rs-radius-xs` | 4px (logo, search) | `rounded-rs-xs` |
| `--rs-radius-2xs` | 3px | `rounded-rs-2xs` |
| `--rs-shadow-card` | `0 2px 6px rgba(16,24,40,.06), 0 12px 32px rgba(16,24,40,.12)` | `shadow-card` (every bordered card) |
| `--rs-shadow-menu` | `0 8px 24px rgba(0,0,0,.14)` | `shadow-menu` |
| `--rs-shadow-popover` | `0 6px 18px rgba(16,24,40,.12)` | `shadow-popover` |
| `--rs-shadow-focus` | `0 0 0 2px` accent | `shadow-focus` |
| `--rs-shadow-bar` | `0 1px 3px rgba(16,24,40,.06)` (phone top bar) | `shadow-rs-bar` |
| `--rs-shadow-bar-up` | `0 -1px 3px rgba(16,24,40,.06)` (phone bottom bar) | `shadow-rs-bar-up` |
| `--rs-content` | 1128px, page content width | `max-w-content` |
| `--rs-gutter` | 16px (12px under 640px), page side padding | `px-gutter` |
| `--rs-menu` | 264px, Me menu width | `w-rs-menu` |

Spacing uses Tailwind's default 4px scale, which covers the design's values (`1.5` = 6px,
`2.5` = 10px, `3.5` = 14px).

### Visual comparison

`npm run ui:compare -- <route> <design-file>` (in `frontend/`, with `make web` running)
screenshots the route and `design/screens/<design-file>.html` at 1280×900, and the route and
`<design-file>_m.html` (or the desktop file when there is none) at 390×844. It saves app,
design and a diff (changed pixels red) side by side in `frontend/ui-compare/` (gitignored)
and prints the share of pixels that differ (any color channel by more than 16).

```sh
npm run ui:compare -- /login Auth
UI_COMPARE_EMAIL=ada@example.org UI_COMPARE_PASSWORD=... npm run ui:compare -- /home Home
```

It drives the installed Google Chrome through `playwright-core`; set `UI_COMPARE_CHROME` to
another executable, `UI_COMPARE_BASE_URL` for another server. For signed-in routes it signs in
with `UI_COMPARE_EMAIL` and `UI_COMPARE_PASSWORD` (a local test account; never commit them).
Design images (`/_blob/…`) are placeholders and always differ.

## Removed in UI.2

The app shell moved to the design. These are gone; do not bring them back:

- `max-w-marketing` and `max-w-page` (1280px); use `max-w-content` through Container.
- The responsive Container padding (16, 24, 32px); it is `px-gutter` (16px, 12px under 640px).
- The system-font `--font-sans` body; the body uses `--rs-font-sans`.
- The fixed 56px signed-in top bar, its `md:` bottom bar and the Events and Admin tabs
  (Events and the admin console are in the Me menu).
- The text-button "Join now" signed-out bar.
- The signed-in three-column footer (`components/app-footer.tsx`) and the old signed-out
  footer row; both are now the site footer.
- The SVG `LogoMark`; the logo is the "RS" square.

## Foundations (legacy)

The tokens below style pages that are not restyled yet. Do not use them in a restyled page.

| Token | Value | Use |
| --- | --- | --- |
| `background` | `#F5F6F4` | Hover tints and landing bands (the page itself is `--rs-surface` white since UI.2) |
| `surface` | `#FFFFFF` | Cards, top bar, bottom bar, inputs; text on primary buttons |
| `border` | `#E1E4E8` | 1px borders and dividers |
| `text` | `#1F2933` | Main text |
| `text-muted` | `#5F6B7A` | Secondary text, hints, inactive tabs |
| `primary` | `#0B7A75` (deep teal) | Primary buttons, outlined buttons, active tab, focus rings |
| `primary-hover` | `#09615D` | Hover and pressed state of `primary` |
| `primary-soft` | `#E6F3F2` | Tinted backgrounds: search box, badges, illustration fills |
| `secondary` | `#1F4E8C` (deep blue) | The wordmark, the landing headline and links in text |
| `danger` | `#C62828` | Field errors, error banners, danger buttons and the border of a danger card |

- **Contrast:** against white, `primary` is about 5.2:1, `text-muted` 5.4:1, `secondary`
  8.3:1 and `danger` 5.6:1; white text on `primary` is 5.2:1 and on `danger` 5.6:1. All pass WCAG AA. Keep new
  text pairs at 4.5:1 or more.
- **Font:** since UI.2 the body is Source Sans 3 (`--rs-font-sans`), 14px, line height 1.45,
  `--rs-text`; the top bar and footer use `font-rs-ui`. Pages that set their own sizes keep
  them until they are restyled.
- **Hierarchy:** one `h1` per page; page titles 24–32px semibold, card titles 16–20px
  semibold.
- **Cards:** the `Card` component: `rs-surface`, `rounded-lg` (8px), 1px `rs-border` and
  `shadow-card`, which sets it apart from the white page (design/screens).
- **Width:** set only by the `Container` component; see [Layout widths](#layout-widths).
- **Theme:** light only (`color-scheme: light`, no `prefers-color-scheme: dark` styles). A
  dark palette is future work and needs its own tokens.

## Layout widths

- **One Container:** `frontend/src/components/Container.tsx` is the only place that sets page
  width. Sections never set their own `max-width`.
- **Widths** (`size` prop, tokens in `globals.css`):

  | Pages | `size` | Max width |
  | --- | --- | --- |
  | Landing and other signed-out (marketing) pages, including auth | `marketing` | 1128px (`max-w-content`) |
  | Signed-in app pages (home, profile, network, projects, events, messaging) | `app` | 1128px (`max-w-content`) |
  | Editor workspace (slice 22) | `full` | none |

  Auth cards are 363px wide and not-found cards about 400px, inside the `marketing`
  Container (see Narrow cards). `app` and `marketing` are both 1128px, as in the design, but stay two
  names, so either can change alone.
- **Page grids:** signed-in pages divide the Container with a grid; only the grid sets
  column widths, never a section.

  | Page | From 1024px | 768–1023px | Below 768px |
  | --- | --- | --- | --- |
  | Profile (`/profile`, `/profile/[userId]`) | main column, then a 320px sidebar: `lg:grid-cols-[minmax(0,1fr)_320px]` | one column, the sidebar under the main column | one column |
  | Home (`/home`) | left 240px, center flexible, right 320px: `lg:grid-cols-[240px_minmax(0,1fr)_320px]` | left 240px and center; the right column moves under the center | one column |

  The gap is 24px (`gap-6`).
- **Side padding:** 16px, 12px under 640px (`px-gutter`; the token drops to 12px, as the
  design's `main{padding:16px 12px 32px}`). Container adds it; sections add none. The full site
  footer keeps 16px under 640px, as in the design.
- **Full-bleed backgrounds:** the hero, CTA band and footer backgrounds span the whole screen.
  Put the background on the `<section>` (or `<footer>`) and its content inside a Container.
- **Fill the width:** grids and cards fill the full Container width.
- **Readable text:** only running paragraphs are capped, at 70ch (`max-w-text`). Section
  headings and intros may be capped at 720px (`max-w-intro`). Never cap the section itself.
  The FAQ list may sit at 720px, centered (`max-w-intro mx-auto`).
- **Feature rows:** two equal columns (image and text) across the full Container width
  (`grid md:grid-cols-2`); they stack below 768px.
- **Card grids:** 3 columns on desktop, 2 on tablet, 1 on phone, with a 24px gap
  (`grid gap-6 md:grid-cols-2 lg:grid-cols-3`).
- **Narrow cards:** auth cards are 363px wide (the column of `design/screens/Auth.html`,
  full width under 640px) and not-found cards about 400px, centered inside the Container.

## Buttons and links

- Buttons are pills (`rounded-full`). Five kinds:
  - **Primary:** `primary` background, `surface` text. One per view, for the main action.
  - **Outlined:** `primary` 1px border and text, transparent background.
  - **Text:** no border; `text-muted` text that darkens on hover, on a `background` tint.
  - **Inverse:** `surface` background, `primary` text. Only on a `primary` band, such as the
    landing call to action.
  - **Danger:** `danger` background, `surface` text. Only for an action that destroys data,
    such as deleting the account, inside a card with a `danger` border.
- Submit buttons in forms are full width.
- Links in text use `secondary` and underline on hover.
- Every interactive element shows a visible focus ring (`focus-visible:ring-2 ring-primary`).

## Signed-out pages

Landing, register, check-email, verify-email, sign-in and reset-password.

- **Top bar** (`components/public-header.tsx`, UI.2): sticky, 52px, `rs-surface` with a
  `rs-nav-border` bottom line, `font-rs-ui`. Left the 34px "RS" square and "ResearchSphere"
  (18px bold `rs-accent`, to `/`). Right, 8px apart, in `<nav aria-label="Visitor">`: an
  Events tab (icon and label, to `/events`, hidden under 640px), "Sign in" as a 36px outline
  pill (`rs-accent`, to `/login`) and "Join now" as a 36px filled pill (to `/register`).
  Button text never wraps, and the pills stay on one line at 390px (18px side padding, 16px
  under 640px); the design wraps them there, which is not copied.
- **Landing (`/`):** first the design's top (`design/screens/Landing.html`, UI.3), in one
  Container with 56px above, 64px below and 56px between (16, 32 and 16px under 640px):
  1. **Hero:** the 52px bold `rs-heading` headline "Find collaborators, write together, and
     pick the right venue." (32px under 640px), a 20px intro (16px under 640px), and
     "Create your research profile" (filled pill, to `/register`) and "Sign in" (outline
     pill, to `/login`). On the right the sample profile card
     (`features/landing/sample-profile-card.tsx`): `sample-cover.svg` cover strip at
     1584:396, a 65px initials avatar, a fictional name and headline, three interest chips
     and "87% research match with you" with a Connect pill that is a picture, not a button.
     It carries a "Sample data" badge. The columns wrap when there is no room for 420px and
     380px; under 640px the card sits 60px below the pills, as on `Landing_m.html`.
  2. **Feature cards:** three cards ("What you can do"): a 28px icon, a 16px/500 title and
     14px text, `repeat(auto-fit, minmax(240px, 1fr))` with a 16px gap, one column under 640px.

  Then the earlier sections, in full-bleed bands alternating `rs-subtle` and `rs-surface`,
  restyled with the `rs-*` tokens:
  3. **The problem:** three cards (slow co-author search, email attachments, venue guesswork).
  4. **How it works:** three numbered steps with icons.
  5. **Feature rows:** matching, the LaTeX editor, venues and the research network, each with
     a title, two lines of text and three bullets. The media column alternates left and right.
  6. **Who it is for:** three cards (students, researchers, labs).
  7. **Use-case story:** one fictional example, labelled "Example".
  8. **FAQ:** native `<details>`/`<summary>`. Answers state only what `docs/SRS.md` requires.
  9. **Call to action:** an `rs-accent-strong` band with "Join now" as an inverse pill.
- **Images:** illustrations in `frontend/public/images/landing/`, shown with `next/image`
  with width, height and alt text that describes them. They lazy-load. The sample card's
  cover is decorative (empty alt). Every image's source and licence is listed in `docs/image-credits.md`.
- **Product previews:** static React mock-ups in `frontend/src/features/landing/` (match
  card, editor, venue list), built from theme tokens. Their names and numbers are fictional
  and each carries a "Sample data" badge. Never show testimonials, user counts or statistics
  that are not real.
- **Experience (FR-4.8):** the card comes above Education. Each entry has a square
  briefcase tile (`primary-soft` with a `primary` icon, no logos), the title in bold,
  "Organization · Employment type", "Mar 2025 - Present · 1 yr 8 mos" (months counted
  inclusive, to the current month while the role is current), "Location · Location type"
  (whichever is set) and the description clamped to 3 lines with "…see more" / "see less".
  On the own profile the header has a "+" that opens the Add form and each entry has a
  pencil that opens its Edit form, which also deletes (confirmed inside the dialog); after
  a delete the focus goes to "+".
- **Footer:** the site footer, the same as on signed-in pages (see [Site footer](#site-footer)).

## Forms

Auth pages follow `design/screens/Auth.html` (UI.3); each route shows the board's card for
its state.

- One card (`features/auth/auth-card.tsx`): 363px, 12px radius, `rs-border` and `shadow-card`,
  24px padding, 14px between items. Its title is the page's h1, styled as the board's card
  heading (16px/500). A subtitle only when it says something the form needs.
- Fields use `Field` with `look="rs"`: a 14px/500 label 4px above a 44px control with a 1px
  `rs-control` border, 6px radius and the `shadow-focus` ring. Hints are 12px `rs-text-muted`
  and errors 12px `rs-danger`, 6px under the control and linked with `aria-describedby`; an
  invalid control gets `aria-invalid` and a 2px `rs-danger` border. No password show/hide (the
  design has none).
- Buttons are `rsButtonClass` pills: the submit is full width and 48px, secondary actions
  44px outline.
- Messages (`features/auth/notice.tsx`), 8px radius, 10px by 12px padding: errors that belong
  to no field at the top of the form (`rs-danger-soft`, `rs-danger-line`, `rs-danger-strong`,
  `role="alert"`); success (`rs-accent-soft`, `rs-heading`) in a `role="status"` region that
  stays mounted; "verify your email first" as a warning (`rs-warning-*`) under the button.
- Links in cards are 14px/500 `rs-accent-strong`, underlined. Under the button, centered and
  muted: "Already on ResearchSphere? Sign in" (register, check-email, verify-email, forgot and
  reset password) or "New here? Join now" (sign-in).
- Other forms (settings, profile dialogs) keep the older `Field` look until they are restyled.

## Signed-in pages

- **Top bar** (`components/app-shell/`, UI.2): sticky, 52px, `rs-surface`, `rs-nav-border`
  bottom line, `font-rs-ui`, inside the 1128px Container. In order:
  - the 34px "RS" square (`aria-label="ResearchSphere home"`, to `/home`);
  - a 34px search field (`rs-search`, 4px radius, up to 280px, "Search researchers,
    projects, posts"), a form that sends `q` to `/search`;
  - the tabs, 80×52px, 24px icon over an 11px label, `rs-nav-muted` (hover `rs-accent`):
    Home (`/home`), My Network (`/connections`), Projects (`/projects`), Messaging
    (`/messaging`), Notifications (`/notifications`) and Me. The current tab is
    `rs-nav-text` with a 2px `rs-accent` underline and `aria-current="page"`. Pages not
    built yet may 404.
  - **Badge:** a tab can show an unread count, a 16px `rs-danger` pill; screen readers hear
    ", N unread". Nothing supplies counts until the messaging and notification slices.
- **Me menu:** the Me tab shows the 24px avatar, "Me" and a chevron, and opens a 264px menu
  (`role="menu"`, `shadow-menu`, 8px radius but the top right): 48px avatar, name and
  headline (the email when there is no headline), a "View profile" outline pill, then
  "Account" (Settings & privacy, Events & venues, Help) and "Manage" (Admin console, only
  for administrators; Sign out). Enter, Space or ArrowDown opens it on the first item,
  ArrowUp on the last; the arrow keys (wrapping), Home and End move; Escape closes it and
  returns to the button; Tab, a link or a click outside closes it.
- **Narrower screens:**
  - 760px and narrower: tabs are 52px wide with icons only (labels stay for screen readers);
    the search collapses to its icon until it has the focus.
  - 640px and narrower: the tabs move to a fixed 56px bottom bar with 10px labels and the
    underline on top; the search fills the top bar; the Me menu opens above the bar, full
    width less 8px.
  - 480px and narrower (the `_m` designs): a fixed 56px phone top bar (36px avatar that opens
    the Me menu, a pill search, a Messaging icon) and a fixed 64px bottom bar with Home,
    Network, Post (to `/home`), Alerts (`/notifications`) and Projects replace the top bar.
- **Logo:** the "RS" square (`components/logo.tsx`), with the name beside it in the
  signed-out bar and the footer, alone in the signed-in bar.
- **Home layout:** the three-column grid of [Page grids](#layout-widths): left the profile
  card (240px), center the feed, right recommendations (320px). The feed and
  recommendations are built by later slices.
- **Home profile card:** the left column of `/home` loads `GET /profiles/me` and shows a
  cover band (the cover image or a gradient), the photo or initials, name, headline, "job title, institution", country and
  "Profile strength: N%" (the same value as the profile sidebar). The name, the avatar and
  the strength line link to `/profile`. A skeleton shows while it loads.
- **Profile header:** on 768px and wider, the right side lists the current institution and
  the latest education entry, each with a square initial badge (`primary-soft`, no logos).
  Beside the pencil or Connect is a "…" (More) button whose menu has "Copy profile link"
  (copies `/profile/<userId>` and shows a toast); ArrowDown opens it, Escape closes it and
  the focus goes back to the button.
- **Sticky profile bar:** once the header card scrolls behind the top bar
  (IntersectionObserver), a slim `surface` bar under the top bar repeats the avatar, the
  name, the headline on one line and the header's buttons.
- **Photo and cover (FR-4.6):** `components/avatar.tsx` is the one avatar: the photo as a
  plain `<img>` (the media URL redirects to a presigned link, so no `next/image`), or the
  initials on a `primary` circle when there is none or it fails to load. Sizes: `sm` top bar,
  `bar` sticky bar and phone top bar, `menu` Me menu, `md` home card, `lg` profile header. Where the name is written beside it,
  the avatar is decorative (`alt=""`). The cover (`features/profile/cover.tsx`) fills the band
  at the top of the header and home cards with `object-cover`, or the `primary`-to-`secondary`
  gradient. On the own profile a camera button sits on the cover's top right and the photo is
  a button; both open one dialog (photo or cover) with a preview cropped as the server crops
  (centre square, centre 4:1), "Upload", "Delete" (confirmed inside the dialog) and "Cancel".
  The own profile is shared by the Me menu, home card and profile page
  (`features/profile/my-profile.ts`), so a change shows everywhere without a reload.
- **Show all:** long lists in profile cards show their first entries (Experience 3,
  Education 2, Skills 6) and then a full-width "Show all N →" row at the bottom of the card
  that expands the list in place (Experience says "Show all N experiences →"). The About card ends with a "Top skills" box: the first 5 skills joined by
  " • ", linking to the Skills card.
- **Footer:** `footerFor` in `components/site-footer.tsx` is the one table of which footer a
  signed-in page has: the home page shows the compact footer card at the bottom of its right
  column, messaging and the editor workspace have none, and every other page ends with the
  full [site footer](#site-footer) after `main`.
- **Unbuilt pages** show a "Coming soon" card that names the requirements the page will
  cover.
- The project workspace (slice 22) is the only page allowed to need more than 768px (SRS 5.1).

## Site footer

`components/site-footer.tsx` (UI.2), on signed-out and signed-in pages: full width,
`rs-footer-bg`, a `rs-nav-border` top line, `font-rs-ui` 12px, line height 1.5, content in the
Container. Six columns, divided by `rs-nav-border` lines:

1. Brand: the 22px "RS" square and "ResearchSphere" (to `/`) and "ResearchSphere © 2026".
2. About: About ResearchSphere, Community guidelines, Privacy & terms, Accessibility.
3. For researchers: Find collaborators (`/connections`), Research projects (`/projects`),
   Events & venues (`/events`), LaTeX workspace.
4. Platform: How AI matching works, Publications & ORCID, Safety center, Developers.
5. Help: Questions? (Help Center), Manage your account and privacy (`/settings/account`),
   Report a problem, each with a muted line under it.
6. "Select language": English only (SRS 2.5); it changes nothing yet.

Links without a page go to `#`. At 960px and narrower the grid has three columns, the brand
takes a full row and the dividers go; at 560px and narrower, two columns.

`<SiteFooter variant="compact" />` is the home page's footer card (`.rs-footer-compact`):
`rs-footer-bg`, 1px `rs-nav-border`, `rounded-card` (10px), 12px padding, centred rows of
About, Accessibility, Help Center, Privacy & terms, Settings (`/settings/account`) and Safety
center, then the "RS" square and "ResearchSphere © 2026".

The API status is not in the design. `features/health/dev-api-status.tsx` shows it in the
bottom left corner only when `NODE_ENV` is `development`, and not at 640px and narrower.

## Icons and images

- Icons are inline SVG drawn for this project: 24px, 1.75px stroke, `currentColor`. There is
  no icon library.
- Decorative images and icons carry `aria-hidden="true"`; icons that are the only content of
  a control need a text label (visible or `sr-only`).
