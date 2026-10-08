import nextJest from "next/jest.js";

// next/jest compiles TypeScript with SWC, mocks CSS and fonts, and loads next.config.ts.
const createJestConfig = nextJest({ dir: "./" });

/** @type {import('jest').Config} */
const config = {
  testEnvironment: "jsdom",
  moduleNameMapper: { "^@/(.*)$": "<rootDir>/src/$1" },
};

export default createJestConfig(config);
