import { defineConfig } from 'vitest/config'
import react, { reactCompilerPreset } from '@vitejs/plugin-react'
import babel from '@rolldown/plugin-babel'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    babel({ presets: [reactCompilerPreset()] }),
    tailwindcss(),
  ],
  server: {
    proxy: {
      // The backend normally sits on :8080, but the browser-QA stack
      // (scripts/qa-env.sh) runs its own throwaway backend on another port so
      // it can't collide with the one you're running by hand — it points this
      // proxy at that one instead.
      "/api": {
        target: process.env.API_PROXY_TARGET ?? "http://localhost:8080",
        // changeOrigin defaults to true when Vite expands the string
        // shorthand, which rewrites the Host header to the backend's own
        // address. The backend's RequestBaseURL (server/internal/caldavserver/
        // handler.go) trusts that header to reconstruct URLs it hands to
        // third parties — e.g. the Google OAuth redirect_uri (#285) — so
        // rewriting it here makes the backend build a callback URL that
        // points at itself instead of at the Vite origin the browser is
        // actually on. Forcing it false preserves the real Host, matching
        // what a production reverse proxy is expected to forward.
        changeOrigin: false,
      },
    },
  },
  // The file extension picks the environment, so neither kind of test has to
  // declare one: `.test.ts` is logic (stores, planners, pure functions) and
  // runs in node, which is why the suite stays fast; `.test.tsx` renders a
  // component and gets jsdom plus Testing Library's cleanup.
  test: {
    projects: [
      {
        extends: true,
        test: {
          name: "logic",
          include: ["src/**/*.test.ts"],
          environment: "node",
        },
      },
      {
        extends: true,
        test: {
          name: "component",
          include: ["src/**/*.test.tsx"],
          environment: "jsdom",
          setupFiles: ["./src/test/setup.ts"],
        },
      },
    ],
  },
})
