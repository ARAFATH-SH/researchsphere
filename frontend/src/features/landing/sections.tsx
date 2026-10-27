import Image from "next/image";
import Link from "next/link";
import type { ReactNode } from "react";

import { Container } from "@/components/Container";
import { Card } from "@/components/ui/card";
import { rsButtonClass } from "@/components/ui/button";

import { EditorPreview } from "./editor-preview";
import { faq } from "./faq";
import {
  CalendarIcon,
  CheckIcon,
  ClockIcon,
  CodeIcon,
  CollaboratorsIcon,
  DiceIcon,
  DocumentIcon,
  FlaskIcon,
  MailIcon,
  PeopleIcon,
  ProfileIcon,
  SparkIcon,
  StudentIcon,
} from "./icons";
import { MatchPreview } from "./match-preview";
import { SampleProfileCard } from "./sample-profile-card";
import { VenuePreview } from "./venue-preview";

// The landing page sections, in page order (docs/ui-guidelines.md, Signed-out pages). Each
// section spans the screen and puts its content in a Container (Layout widths). Welcome is
// the design's hero and cards; the sections after it came before the design (UI.3).

// The illustrations are 1376 × 768.
const imageSize = { width: 1376, height: 768 };

// Welcome is the top of the page as in design/screens/Landing.html: the hero and the three
// feature cards, 56px apart, with 56px above and 64px below (16px apart, 16px and 32px under
// 640px).
export function Welcome() {
  return (
    <Container
      size="marketing"
      className="flex flex-col gap-14 pt-14 pb-16 max-[640px]:gap-4 max-[640px]:pt-4 max-[640px]:pb-8"
    >
      <Hero />
      <Highlights />
    </Container>
  );
}

// Hero is the 52px headline, the intro, the two pills and the sample profile card. The two
// columns wrap to one when there is no room for both (420px and 380px at least); under 640px
// the card sits 60px below the pills, as measured on Landing_m.html.
function Hero() {
  return (
    <section aria-labelledby="hero-heading" className="flex flex-wrap items-center gap-10 max-[640px]:gap-y-15">
      <div className="flex min-w-0 flex-[1_1_420px] flex-col gap-5">
        <h1
          id="hero-heading"
          className="text-rs-display leading-rs-tight font-bold text-rs-heading max-[640px]:text-rs-h1"
        >
          Find collaborators, write together, and pick the right venue.
        </h1>
        <p className="text-rs-title leading-rs-relaxed max-[640px]:text-rs-lead">
          A professional network built only for researchers and students — research profiles, AI-matched
          collaborators and projects, a shared LaTeX editor and a call-for-papers tracker in one place.
        </p>
        <div className="flex flex-wrap gap-3">
          <Link href="/register" className={rsButtonClass({ size: "lg" })}>
            Create your research profile
          </Link>
          <Link href="/login" className={rsButtonClass({ variant: "outline", size: "lg" })}>
            Sign in
          </Link>
        </div>
      </div>
      <SampleProfileCard className="flex-[1_1_380px]" />
    </section>
  );
}

const highlights = [
  {
    icon: <CollaboratorsIcon />,
    title: "AI-matched collaborators",
    text: "Matches by research meaning, not citation counts — so students without papers get matches too.",
  },
  {
    icon: <CodeIcon />,
    title: "Shared LaTeX workspace",
    text: "Up to six co-authors edit live, compile to PDF, and get plain-language help with errors.",
  },
  {
    icon: <CalendarIcon />,
    title: "Venues that fit your paper",
    text: "Conferences and journals ranked by scope match, with deadlines you can filter.",
  },
];

// Highlights are the three feature cards under the hero, as many to a row as fit at 240px.
function Highlights() {
  return (
    <section
      aria-label="What you can do"
      className="grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-4 max-[640px]:grid-cols-1"
    >
      {highlights.map((h) => (
        <div
          key={h.title}
          className="flex flex-col gap-2 rounded-card border border-rs-border bg-rs-surface p-5 shadow-card"
        >
          <span className="text-rs-accent-strong">{h.icon}</span>
          <h2 className="text-rs-lead leading-rs-snug font-medium">{h.title}</h2>
          <p>{h.text}</p>
        </div>
      ))}
    </section>
  );
}

// SectionHeading is a section's h2 with an optional intro, capped at 720px.
function SectionHeading({ id, title, intro }: { id: string; title: string; intro?: string }) {
  return (
    <div className="max-w-intro space-y-3">
      <h2 id={id} className="text-rs-h3 leading-rs-tight font-bold text-rs-heading md:text-rs-h2">
        {title}
      </h2>
      {intro ? <p className="text-lg text-rs-text-muted">{intro}</p> : null}
    </div>
  );
}

// Band is one full-bleed section with its content in the marketing Container.
function Band({ labelledBy, tone = "background", children }: {
  labelledBy: string;
  tone?: "background" | "surface";
  children: ReactNode;
}) {
  return (
    <section aria-labelledby={labelledBy} className={tone === "surface" ? "bg-rs-surface" : "bg-rs-subtle"}>
      <Container size="marketing" className="space-y-10 py-14 md:py-20">
        {children}
      </Container>
    </section>
  );
}

const problems = [
  {
    icon: <ClockIcon />,
    title: "Finding the right co-author is slow",
    text: "Searching by name and asking around rarely turns up the people whose work fits yours.",
  },
  {
    icon: <MailIcon />,
    title: "Papers are written over email attachments",
    text: "Every round of edits means another file version and another merge by hand.",
  },
  {
    icon: <DiceIcon />,
    title: "Choosing a venue is guesswork",
    text: "Scopes and deadlines are spread over many sites, so the right venue is easy to miss.",
  },
];

export function Problem() {
  return (
    <Band labelledBy="problem-heading">
      <SectionHeading id="problem-heading" title="Research together is harder than it should be" />
      <ul className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
        {problems.map((p) => (
          <li key={p.title}>
            <Card className="h-full space-y-3 p-6">
              <span className="inline-flex rounded-full bg-rs-accent-soft p-2.5 text-rs-accent-strong">{p.icon}</span>
              <h3 className="text-lg font-medium">{p.title}</h3>
              <p className="text-rs-text-muted">{p.text}</p>
            </Card>
          </li>
        ))}
      </ul>
    </Band>
  );
}

const steps = [
  {
    icon: <ProfileIcon />,
    title: "Build your research profile",
    text: "Add your interests, skills and education, and import your publications from ORCID or by DOI.",
  },
  {
    icon: <SparkIcon />,
    title: "Get AI-matched collaborators and projects",
    text: "See the researchers and open projects whose work is closest to yours, and why they match.",
  },
  {
    icon: <DocumentIcon />,
    title: "Write, compile and submit together",
    text: "Edit LaTeX with your co-authors in real time, compile the PDF and pick a venue before its deadline.",
  },
];

export function HowItWorks() {
  return (
    <Band labelledBy="how-heading" tone="surface">
      <SectionHeading id="how-heading" title="How it works" />
      <ol className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
        {steps.map((step, i) => (
          <li key={step.title} className="space-y-3">
            <div className="flex items-center gap-3">
              <span
                className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-rs-accent-strong text-lg font-medium text-rs-surface"
                aria-hidden="true"
              >
                {i + 1}
              </span>
              <span className="text-rs-accent-strong">{step.icon}</span>
            </div>
            <h3 className="text-lg font-medium">
              <span className="sr-only">Step {i + 1}: </span>
              {step.title}
            </h3>
            <p className="text-rs-text-muted">{step.text}</p>
          </li>
        ))}
      </ol>
    </Band>
  );
}

type Feature = { id: string; title: string; text: string; points: string[]; media: ReactNode };

const features: Feature[] = [
  {
    id: "feature-matching",
    title: "AI collaborator matching",
    text: "ResearchSphere reads what your research is about and suggests the people and projects that fit it, with the topics you share.",
    points: [
      "Ten suggested researchers with a match percentage",
      "Up to three shared interests or skills for each",
      "Not ranked by citation, publication or connection counts",
    ],
    media: <MatchPreview />,
  },
  {
    id: "feature-editor",
    title: "Real-time LaTeX editor for up to 6 co-authors",
    text: "Write in one shared project instead of trading files. Everyone sees each change as it happens, and nothing is lost.",
    points: [
      "Each co-author's cursor and name in their own colour",
      "Saves automatically and compiles to PDF beside the code",
      "Errors listed with their file and line",
    ],
    media: <EditorPreview />,
  },
  {
    id: "feature-venues",
    title: "Venue and event recommendations with deadlines",
    text: "Give your project an abstract and see the venues whose scope fits it best, ordered by how well they match.",
    points: [
      "Five venues with a fit score and submission deadline",
      "Only venues whose deadline has not passed",
      "Filter events by type, field and deadline",
    ],
    media: <VenuePreview />,
  },
  {
    id: "feature-network",
    title: "Research network",
    text: "Keep your profile, publications and connections in one place, and follow what the people you work with are sharing.",
    points: [
      "Profiles with interests, skills, education and publications",
      "Connections and requests between researchers",
      "A feed of posts from your connections",
    ],
    media: (
      <Image
        src="/images/landing/writing-v3.jpg"
        {...imageSize}
        sizes="(min-width: 768px) 50vw, 100vw"
        alt="Illustration of researchers around a large screen showing a LaTeX document, with charts, a checklist and a magnifying glass"
        className="h-auto w-full rounded-lg"
      />
    ),
  },
];

export function Features() {
  return (
    <Band labelledBy="features-heading">
      <SectionHeading
        id="features-heading"
        title="Everything a research team needs"
        intro="From the first introduction to the final PDF, in one place."
      />
      <div className="space-y-16 md:space-y-20">
        {features.map((f, i) => (
          <article key={f.id} aria-labelledby={f.id} className="grid items-center gap-8 md:grid-cols-2 md:gap-12">
            <div className={`space-y-4 ${i % 2 === 1 ? "md:order-last" : ""}`}>
              <h3 id={f.id} className="text-xl font-medium md:text-2xl">
                {f.title}
              </h3>
              <p className="max-w-text text-rs-text-muted">{f.text}</p>
              <ul className="space-y-2">
                {f.points.map((point) => (
                  <li key={point} className="flex gap-2">
                    <CheckIcon />
                    <span>{point}</span>
                  </li>
                ))}
              </ul>
            </div>
            <div>{f.media}</div>
          </article>
        ))}
      </div>
    </Band>
  );
}

const audiences = [
  {
    icon: <StudentIcon />,
    title: "Students",
    text: "Looking for a supervisor or a team: find researchers and open projects that match your interests, even before your first publication.",
  },
  {
    icon: <PeopleIcon />,
    title: "Researchers",
    text: "Looking for co-authors: meet people whose work fits yours, then write the paper with them in one shared editor.",
  },
  {
    icon: <FlaskIcon />,
    title: "Labs",
    text: "Running projects: post a project with the skills it needs, review requests to join and give each member the right role.",
  },
];

export function Audience() {
  return (
    <Band labelledBy="audience-heading" tone="surface">
      <SectionHeading id="audience-heading" title="Who it is for" />
      <ul className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
        {audiences.map((a) => (
          <li key={a.title}>
            <Card className="h-full space-y-3 p-6">
              <span className="inline-flex rounded-full bg-rs-accent-soft p-2.5 text-rs-accent-strong">{a.icon}</span>
              <h3 className="text-lg font-medium">{a.title}</h3>
              <p className="text-rs-text-muted">{a.text}</p>
            </Card>
          </li>
        ))}
      </ul>
    </Band>
  );
}

const storySteps = [
  "Lina, a master's student, signs up and fills in her profile with her interests, skills and her thesis abstract.",
  "ResearchSphere matches her with two researchers who work on the same questions. She sends both a connection request, and they accept.",
  "They start a project and write the paper together in the shared LaTeX editor, each seeing the others' changes as they type.",
  "With the abstract in place, they compare the recommended venues and pick one whose submission deadline is six weeks away.",
];

export function Story() {
  return (
    <Band labelledBy="story-heading">
      <div className="grid items-center gap-8 md:grid-cols-2 md:gap-12">
        <div className="space-y-5">
          <p className="inline-block rounded-full bg-rs-accent-soft px-3 py-1 text-sm font-medium text-rs-accent-strong">
            Example
          </p>
          <SectionHeading id="story-heading" title="From a profile to a paper" />
          <ol className="space-y-4">
            {storySteps.map((step, i) => (
              <li key={step} className="flex gap-3">
                <span
                  className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full border border-rs-accent-strong text-sm font-medium text-rs-accent-strong"
                  aria-hidden="true"
                >
                  {i + 1}
                </span>
                <p className="max-w-text">{step}</p>
              </li>
            ))}
          </ol>
          <p className="text-sm text-rs-text-muted">An illustration of how the features fit together. Lina is not a real user.</p>
        </div>
        <Image
          src="/images/landing/conference.jpg"
          {...imageSize}
          sizes="(min-width: 768px) 50vw, 100vw"
          alt="Illustration of a speaker presenting charts at a conference to an audience taking notes"
          className="h-auto w-full rounded-lg"
        />
      </div>
    </Band>
  );
}

export function Faq() {
  return (
    <Band labelledBy="faq-heading" tone="surface">
      <div className="mx-auto max-w-intro space-y-8">
        <SectionHeading id="faq-heading" title="Frequently asked questions" />
        <div className="divide-y divide-rs-border rounded-lg border border-rs-border">
          {faq.map((entry) => (
            <details key={entry.question} className="group">
              <summary className="flex cursor-pointer list-none items-center justify-between gap-4 p-5 font-medium focus-visible:shadow-[inset_0_0_0_2px_var(--rs-accent)] focus-visible:outline-none [&::-webkit-details-marker]:hidden">
                <h3>{entry.question}</h3>
                <span className="text-xl text-rs-accent-strong transition-transform group-open:rotate-45" aria-hidden="true">
                  +
                </span>
              </summary>
              <p className="max-w-text px-5 pb-5 text-rs-text-muted">{entry.answer}</p>
            </details>
          ))}
        </div>
      </div>
    </Band>
  );
}

export function CallToAction() {
  return (
    <section aria-labelledby="cta-heading" className="bg-rs-accent-strong">
      <Container size="marketing" className="flex flex-col items-start gap-6 py-14 md:flex-row md:items-center md:justify-between md:py-16">
        <div className="max-w-intro space-y-2">
          <h2 id="cta-heading" className="text-rs-h3 leading-rs-tight font-bold text-rs-surface md:text-rs-h2">
            Start your research network
          </h2>
          <p className="text-lg text-rs-surface">Build your profile and meet researchers whose work fits yours.</p>
        </div>
        <Link href="/register" className={rsButtonClass({ variant: "inverse", size: "lg" })}>
          Join now
        </Link>
      </Container>
    </section>
  );
}
