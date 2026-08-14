// Runs before every `.test.tsx` (vite.config.ts's "component" project).
//
// jest-dom adds the DOM matchers the component tests read as assertions —
// toBeDisabled, toBeInTheDocument — and cleanup unmounts between tests, which
// Testing Library only does for itself when Vitest globals are on. They are
// not here, so it is wired explicitly.
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

afterEach(cleanup);
