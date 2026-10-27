import { AccountDeletedNotice } from "@/features/auth/account-deleted";
import { Audience, CallToAction, Faq, Features, HowItWorks, Problem, Story, Welcome } from "@/features/landing/sections";

// The landing page (docs/ui-guidelines.md, Signed-out pages): the design's hero and feature
// cards, then the earlier sections. The footer comes from the layout.
export default function Page() {
  return (
    <>
      <AccountDeletedNotice />
      <Welcome />
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
