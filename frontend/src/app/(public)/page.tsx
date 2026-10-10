import { Audience, CallToAction, Faq, Features, Hero, HowItWorks, Problem, Story } from "@/features/landing/sections";

// The landing page (docs/ui-guidelines.md, Signed-out pages). The footer comes from the layout.
export default function Page() {
  return (
    <>
      <Hero />
      <Problem />
      <HowItWorks />
      <Features />
      <Audience />
      <Story />
      <Faq />
      <CallToAction />
    </>
  );
}
