import Link from "next/link";

export default function NotFound() {
  return (
    <section className="space-y-3">
      <h1 className="text-2xl font-semibold">Page not found</h1>
      <p>
        <Link href="/" className="underline">
          Go to the start page
        </Link>
      </p>
    </section>
  );
}
