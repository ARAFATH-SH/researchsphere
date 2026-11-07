import { format } from "node:util";

// Fails any test that calls console.error, such as React's "not wrapped in act(...)" warning.
// The default multi-file run does not show console output, so without this such a warning
// passes unnoticed. To read the output, run `npx jest --verbose`.

// jsdom has neither IntersectionObserver nor requestIdleCallback, so next/link falls back to a
// timer that sets state about 1ms after mount (its viewport prefetch), which React reports as
// an update outside act(). An idle callback that never fires turns that off. Stubbing
// IntersectionObserver instead would send next/link to the mock observers tests install.
if (typeof window !== "undefined") window.requestIdleCallback = () => 0;

// allowed holds patterns for known third-party noise. Give each one a comment naming its
// source and why it cannot be fixed here. A warning from our own code is fixed, not listed.
const allowed: RegExp[] = [];

const captured: string[] = [];
const realConsoleError = console.error;
// Taken before any test can install fake timers, which would stall the wait below.
const realSetTimeout = globalThis.setTimeout;

// A plain assignment rather than jest.spyOn, so jest.restoreAllMocks cannot remove the guard.
// A test's own spy wraps this one: it still counts unless the spy replaces the implementation.
console.error = (...args: unknown[]) => {
  realConsoleError(...args);
  const message = format(...args);
  if (!allowed.some((pattern) => pattern.test(message))) captured.push(message);
};

function failOnConsoleError(): void {
  if (captured.length === 0) return;
  const messages = captured.splice(0).join("\n\n");
  throw new Error(`console.error was called (jest.setup.ts):\n\n${messages}`);
}

// Hooks of one block run in the order they were defined, so this runs before Testing Library's
// cleanup as long as this file does not import it. The wait lets promises a test left pending
// settle while its components are mounted; after cleanup their late update would warn nobody.
afterEach(async () => {
  await new Promise((resolve) => realSetTimeout(resolve, 0));
  failOnConsoleError();
});

// Catches a call made after the last test's check, such as in a test file's afterEach or
// during cleanup. A call made that late in an earlier test is reported against the next test.
// A test file's own afterAll runs after this one, so a call made there is not caught.
afterAll(failOnConsoleError);
