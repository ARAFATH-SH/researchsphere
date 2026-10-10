// Answers to the landing page FAQ. Each one states only what docs/SRS.md requires; the IDs
// in the comments are the requirements it rests on.
export type FaqEntry = { question: string; answer: string };

export const faq: FaqEntry[] = [
  {
    question: "Is it free?",
    // The SRS does not cover pricing; this answer waits for the product owner.
    answer: "[Answer pending: the requirements do not cover pricing yet.]",
  },
  {
    // FR-4.5, FR-5.5, section 2.2 (visitors see only the landing page), FR-2.8.
    question: "Who can see my profile?",
    answer:
      "Only people who are signed in to ResearchSphere. Visitors who are not signed in see this page and nothing else. " +
      "Your profile shows your interests, skills, education and only the publications you have confirmed. " +
      "You can delete your account, and with it your profile, posts and connections, at any time.",
  },
  {
    // AI-1.1, AI-2.1, AI-2.2, AI-2.4, AI-4.1, section 2.5.
    question: "How does AI matching work?",
    answer:
      "A pretrained language model turns your interests, skills, headline, bio and the titles and abstracts of your " +
      "confirmed publications into a summary of what your research is about. ResearchSphere then suggests the 10 " +
      "researchers whose profiles are most similar, each with a match percentage and up to 3 topics you share. " +
      "It never ranks people by citation, publication or connection counts, and one interest or skill is enough to get suggestions.",
  },
  {
    // FR-10.4, FR-12.1, FR-12.2, FR-13.1.
    question: "Can I use my existing LaTeX files?",
    answer:
      "Yes. Every project starts with a main.tex file, and you can create more .tex files and folders and paste your " +
      "existing source in. You can upload figures as PNG, JPG or PDF files of up to 5 MB each. Papers compile with pdfLaTeX and BibTeX.",
  },
  {
    // Section 2.5 (nothing is trained or fine-tuned), AI-7.2, AI-8.3, NFR-9.1.
    question: "Is my data used to train AI?",
    answer:
      "No. ResearchSphere uses pretrained models and does not train or fine-tune any model. The optional writing " +
      "assistant sends the text you ask it about to an outside language model, and it asks for your agreement before the first time it does.",
  },
];
