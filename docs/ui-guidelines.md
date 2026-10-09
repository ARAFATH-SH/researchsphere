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
| `danger` | `#C62828` | Field errors and error banners |

- **Contrast:** against white, `primary` is about 5.2:1, `text-muted` 5.4:1, `secondary`
  8.3:1 and `danger` 5.6:1; white text on `primary` is 5.2:1. All pass WCAG AA. Keep new
  text pairs at 4.5:1 or more.
- **Font:** the system stack (`-apple-system, system-ui, "Segoe UI", Roboto, …`). Body text
  is 16px in forms and 14–16px elsewhere; secondary text 14px.
- **Hierarchy:** one `h1` per page; page titles 24–32px semibold, card titles 16–20px
  semibold.
- **Cards:** `surface`, `rounded-lg` (8px), 1px `border`, no heavy shadows (at most
  `shadow-sm`).
- **Width:** content is centered with a maximum width of 1128px (`max-w-page`) and 16px side
  padding.
- **Theme:** light only (`color-scheme: light`, no `prefers-color-scheme: dark` styles). A
  dark palette is future work and needs its own tokens.

## Buttons and links

- Buttons are pills (`rounded-full`). Three kinds:
  - **Primary:** `primary` background, `surface` text. One per view, for the main action.
  - **Outlined:** `primary` 1px border and text, transparent background.
  - **Text:** no border; `text-muted` text that darkens on hover, on a `background` tint.
- Submit buttons in forms are full width.
- Links in text use `secondary` and underline on hover.
- Every interactive element shows a visible focus ring (`focus-visible:ring-2 ring-primary`).

## Signed-out pages

Landing, register, check-email, verify-email, sign-in and reset-password.

- **Top bar:** logo on the left; on the right, "Join now" as a text button (to `/register`)
  and "Sign in" as an outlined pill (to `/login`).
- **Landing (`/`):** a two-column hero. Left: the headline "Find collaborators. Write papers
  together.", one line of supporting text, "Join now" (primary pill) and "Sign in" (outlined
  pill). Right: an illustration area made of simple shapes of our own (no copied images).
  Below 768px the columns stack, text first.
- **Footer:** the product name and the API status.

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
- **Home layout** (reserved now, built later): a three-column grid — left a profile card
  (about 225px), center the feed, right recommendations (about 300px). Below 1024px the right
  column moves under the feed; below 768px everything is one column.
- **Unbuilt pages** show a "Coming soon" card that names the requirements the page will
  cover.
- The project workspace (slice 22) is the only page allowed to need more than 768px (SRS 5.1).

## Icons and images

- Icons are inline SVG drawn for this project: 24px, 1.75px stroke, `currentColor`. There is
  no icon library.
- Decorative images and icons carry `aria-hidden="true"`; icons that are the only content of
  a control need a text label (visible or `sr-only`).
