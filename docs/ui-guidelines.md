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
| `--rs-content` | 1128px, page content width | `max-w-content` |
| `--rs-gutter` | 16px, page side padding | `px-gutter` |
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

## Foundations (legacy)

The tokens below style pages that are not restyled yet. Do not use them in a restyled page.

| Token | Value | Use |
| --- | --- | --- |
| `background` | `#F5F6F4` | Page background |
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
- **Font:** the system stack (`-apple-system, system-ui, "Segoe UI", Roboto, …`). Body text
  is 16px in forms and 14–16px elsewhere; secondary text 14px.
- **Hierarchy:** one `h1` per page; page titles 24–32px semibold, card titles 16–20px
  semibold.
- **Cards:** `surface`, `rounded-lg` (8px), 1px `border`, no heavy shadows (at most
  `shadow-sm`).
- **Width:** set only by the `Container` component; see [Layout widths](#layout-widths).
- **Theme:** light only (`color-scheme: light`, no `prefers-color-scheme: dark` styles). A
  dark palette is future work and needs its own tokens.

## Layout widths

- **One Container:** `frontend/src/components/Container.tsx` is the only place that sets page
  width. Sections never set their own `max-width`.
- **Widths** (`size` prop, tokens in `globals.css`):

  | Pages | `size` | Max width |
  | --- | --- | --- |
  | Landing and other signed-out (marketing) pages, including auth | `marketing` | 1280px (`max-w-marketing`) |
  | Signed-in app pages (home, profile, network, projects, events, messaging) | `app` | 1280px (`max-w-page`) |
  | Editor workspace (slice 22) | `full` | none |

  Auth and not-found cards stay about 400px wide inside the `marketing` Container (see
  Narrow cards). `app` and `marketing` are both 1280px but stay two tokens, so either can
  change alone.
- **Page grids:** signed-in pages divide the Container with a grid; only the grid sets
  column widths, never a section.

  | Page | From 1024px | 768–1023px | Below 768px |
  | --- | --- | --- | --- |
  | Profile (`/profile`, `/profile/[userId]`) | main column, then a 320px sidebar: `lg:grid-cols-[minmax(0,1fr)_320px]` | one column, the sidebar under the main column | one column |
  | Home (`/home`) | left 240px, center flexible, right 320px: `lg:grid-cols-[240px_minmax(0,1fr)_320px]` | left 240px and center; the right column moves under the center | one column |

  The gap is 24px (`gap-6`).
- **Side padding:** 16px on phones, 24px from 768px (tablets), 32px from 1024px (desktop).
  Container adds it; sections add none.
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
- **Narrow cards:** auth and not-found cards stay about 400px wide, centered inside the
  Container.

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

- **Top bar:** logo on the left; on the right, "Join now" as a text button (to `/register`)
  and "Sign in" as an outlined pill (to `/login`).
- **Landing (`/`):** sections in this order, alternating `surface` and `background` bands:
  1. **Hero:** the headline "Find collaborators. Write papers together.", one line of
     supporting text, "Join now" (primary pill) and "Sign in" (outlined pill). On the right is
     `hero-team.jpg`, the only image that loads eagerly (`preload`). Below 768px the columns
     stack, text first.
  2. **The problem:** three cards (slow co-author search, email attachments, venue guesswork).
  3. **How it works:** three numbered steps with icons.
  4. **Feature rows:** matching, the LaTeX editor, venues and the research network, each with
     a title, two lines of text and three bullets. The media column alternates left and right.
  5. **Who it is for:** three cards (students, researchers, labs).
  6. **Use-case story:** one fictional example, labelled "Example".
  7. **FAQ:** native `<details>`/`<summary>`. Answers state only what `docs/SRS.md` requires.
  8. **Call to action:** a `primary` band with "Join now" as an `inverse` pill (`surface`
     background, `primary` text).
- **Images:** illustrations in `frontend/public/images/landing/`, shown with `next/image`
  with width, height and alt text that describes them. They lazy-load, except the hero
  image. Every image's source and licence is listed in `docs/image-credits.md`.
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
- **Footer:** `surface` with a top border: About, Privacy, Terms and Contact links, "©
  ResearchSphere" and the API status.

## Forms

- One centered card, about 400px wide, with a title and a short line of text.
- Labels above inputs; inputs full width with a 1px border and 8px radius.
- Hints in `text-muted` 14px under the input; errors in `danger` 14px under the field, linked
  with `aria-describedby`, and the input gets `aria-invalid`.
- Errors that belong to no field go in a banner at the top of the card (`role="alert"`).
- A full-width primary pill submit button.
- Under the button: "Already on ResearchSphere? Sign in" (on the register, check-email and
  verify-email cards).

## Signed-in pages

- **Top bar:** fixed, `surface`, 1px bottom border. Logo, a search box (to `/search`), and icon
  tabs with labels: Home (`/home`), Network (`/connections`), Projects (`/projects`), Events
  (`/events`), Notifications (`/notifications`), Me (`/profile`). The active tab is
  `primary` with a 2px underline and `aria-current="page"`.
- **Below 768px** the tabs move to a fixed bottom bar; the top bar keeps the logo and search.
- **Logo:** the mark plus the "ResearchSphere" wordmark. To leave room for search and tabs,
  the signed-in top bar shows only the mark below 1024px, and the signed-out top bar below
  640px; the name stays available to screen readers. Buttons never wrap their text.
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
  `bar` sticky bar, `md` home card, `lg` profile header. Where the name is written beside it,
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
- **Footer:** every signed-in page except the editor workspace ends with the signed-in footer
  inside the main Container (not full-bleed): three columns of small `text-muted` links,
  three help rows with icons (Help Center, Settings at `/settings/account`, AI
  transparency), a Language select with only English (SRS 2.5) and "ResearchSphere © 2026"
  with the logo mark. Links without a page go to `#`.
- **Unbuilt pages** show a "Coming soon" card that names the requirements the page will
  cover.
- The project workspace (slice 22) is the only page allowed to need more than 768px (SRS 5.1).

## Icons and images

- Icons are inline SVG drawn for this project: 24px, 1.75px stroke, `currentColor`. There is
  no icon library.
- Decorative images and icons carry `aria-hidden="true"`; icons that are the only content of
  a control need a text label (visible or `sr-only`).
