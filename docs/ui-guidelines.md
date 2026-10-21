# UI guidelines

How ResearchSphere looks. The layout and patterns follow familiar professional and research
networks (white cards on a light page, a top bar with icon tabs), but the name, logo and
colors are our own. **Never copy another product's logo, name, wordmark, images or exact
brand colors** (for example LinkedIn's or ResearchGate's).

The tokens below are Tailwind theme tokens in `frontend/src/app/globals.css` (`@theme`).
Components use only these names, for example `bg-background`, `bg-surface`,
`border-border`, `text-text-muted`, `bg-primary`, `text-secondary`. Never write raw colors
(hex values, `white`, `gray-400` …) in components.

## Foundations

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
- **Show all:** long lists in profile cards show their first entries (Education 2, Skills
  6) and then a full-width "Show all N →" row at the bottom of the card that expands the
  list in place. The About card ends with a "Top skills" box: the first 5 skills joined by
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
