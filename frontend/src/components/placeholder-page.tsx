type PlaceholderPageProps = {
  title: string;
  purpose: string;
  requirements: string[];
  note?: string;
};

// PlaceholderPage stands in for a page from SRS 5.1 until its slice builds it.
export function PlaceholderPage({ title, purpose, requirements, note }: PlaceholderPageProps) {
  return (
    <section className="space-y-3">
      <h1 className="text-2xl font-semibold">{title}</h1>
      <p>{purpose}</p>
      <p className="text-sm text-gray-600 dark:text-gray-400">
        Placeholder. Built in a later slice: {requirements.join(", ")}.
        {note ? ` ${note}` : null}
      </p>
    </section>
  );
}
