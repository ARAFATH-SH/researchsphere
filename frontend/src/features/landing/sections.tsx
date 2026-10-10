import Image from "next/image";
import Link from "next/link";
import type { ReactNode } from "react";

import { Container } from "@/components/Container";
import { Card } from "@/components/ui/card";
import { buttonClass } from "@/components/ui/button";

import { EditorPreview } from "./editor-preview";
import { faq } from "./faq";
import {
  CheckIcon,
  ClockIcon,
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
import { VenuePreview } from "./venue-preview";

// The landing page sections, in page order (docs/ui-guidelines.md, Signed-out pages). Each
// section spans the screen and puts its content in a Container (Layout widths).

// The illustrations are 1376 × 768.
const imageSize = { width: 1376, height: 768 };

export function Hero() {
  return (
    <section className="bg-surface">
      <Container size="marketing" className="grid items-center gap-10 py-10 md:grid-cols-2 md:py-16">
        <div className="space-y-6">
          <h1 className="text-4xl leading-tight font-light text-secondary md:text-5xl">
            Find collaborators. Write papers together.
          </h1>
          <p className="max-w-text text-lg text-text-muted">
            Build your research profile, meet researchers whose work fits yours, write in one shared
            LaTeX editor and find the right venue for your paper.
          </p>
          <div className="flex max-w-sm flex-col gap-3 sm:flex-row">
            <Link href="/register" className={buttonClass({ variant: "primary", full: true })}>
              Join now
            </Link>
            <Link href="/login" className={buttonClass({ variant: "outline", full: true })}>
              Sign in
            </Link>
          </div>
        </div>
        <Image
          src="/images/landing/hero-team.jpg"
          {...imageSize}
          preload
          sizes="(min-width: 768px) 50vw, 100vw"
          alt="Illustration of four students working together at a table with a laptop, notebooks and books"
          className="h-auto w-full rounded-lg"
        />
      </Container>
    </section>
  );
}

// SectionHeading is a section's h2 with an optional intro, capped at 720px.
function SectionHeading({ id, title, intro }: { id: string; title: string; intro?: string }) {
  return (
    <div className="max-w-intro space-y-3">
      <h2 id={id} className="text-2xl font-semibold md:text-3xl">
        {title}
      </h2>
      {intro ? <p className="text-lg text-text-muted">{intro}</p> : null}
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
    <section aria-labelledby={labelledBy} className={tone === "surface" ? "bg-surface" : "bg-background"}>
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
              <span className="inline-flex rounded-full bg-primary-soft p-2.5 text-primary">{p.icon}</span>
              <h3 className="text-lg font-semibold">{p.title}</h3>
              <p className="text-text-muted">{p.text}</p>
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
                className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-primary text-lg font-semibold text-surface"
                aria-hidden="true"
              >
                {i + 1}
              </span>
              <span className="text-primary">{step.icon}</span>
            </div>
            <h3 className="text-lg font-semibold">
              <span className="sr-only">Step {i + 1}: </span>
              {step.title}
            </h3>
            <p className="text-text-muted">{step.text}</p>
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
              <h3 id={f.id} className="text-xl font-semibold md:text-2xl">
                {f.title}
              </h3>
              <p className="max-w-text text-text-muted">{f.text}</p>
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
              <span className="inline-flex rounded-full bg-primary-soft p-2.5 text-primary">{a.icon}</span>
              <h3 className="text-lg font-semibold">{a.title}</h3>
              <p className="text-text-muted">{a.text}</p>
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
          <p className="inline-block rounded-full bg-primary-soft px-3 py-1 text-sm font-semibold text-primary">
            Example
          </p>
          <SectionHeading id="story-heading" title="From a profile to a paper" />
          <ol className="space-y-4">
            {storySteps.map((step, i) => (
              <li key={step} className="flex gap-3">
                <span
                  className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full border border-primary text-sm font-semibold text-primary"
                  aria-hidden="true"
                >
                  {i + 1}
                </span>
                <p className="max-w-text">{step}</p>
              </li>
            ))}
          </ol>
          <p className="text-sm text-text-muted">An illustration of how the features fit together. Lina is not a real user.</p>
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
        <div className="divide-y divide-border rounded-lg border border-border">
          {faq.map((entry) => (
            <details key={entry.question} className="group">
              <summary className="flex cursor-pointer list-none items-center justify-between gap-4 p-5 font-semibold focus-visible:ring-2 focus-visible:ring-primary focus-visible:outline-none focus-visible:ring-inset [&::-webkit-details-marker]:hidden">
                <h3>{entry.question}</h3>
                <span className="text-xl text-primary transition-transform group-open:rotate-45" aria-hidden="true">
                  +
                </span>
              </summary>
              <p className="max-w-text px-5 pb-5 text-text-muted">{entry.answer}</p>
            </details>
          ))}
        </div>
      </div>
    </Band>
  );
}

export function CallToAction() {
  return (
    <section aria-labelledby="cta-heading" className="bg-primary">
      <Container size="marketing" className="flex flex-col items-start gap-6 py-14 md:flex-row md:items-center md:justify-between md:py-16">
        <div className="max-w-intro space-y-2">
          <h2 id="cta-heading" className="text-2xl font-semibold text-surface md:text-3xl">
            Start your research network
          </h2>
          <p className="text-lg text-surface">Build your profile and meet researchers whose work fits yours.</p>
        </div>
        <Link href="/register" className={buttonClass({ variant: "inverse" })}>
          Join now
        </Link>
      </Container>
    </section>
  );
}
