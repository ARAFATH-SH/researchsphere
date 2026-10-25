// toastDuration is how long a toast stays, in milliseconds.
export const toastDuration = 3000;

// Toast is a short message at the bottom of the screen, such as "Saved". It is a live
// status, so screen readers announce it; whoever shows it removes it after toastDuration.
export function Toast({ message }: { message: string }) {
  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-20 z-50 flex justify-center px-4 md:bottom-6">
      <p
        role="status"
        className="rounded-full bg-text px-4 py-2 text-sm font-semibold text-surface shadow-sm"
      >
        {message}
      </p>
    </div>
  );
}
