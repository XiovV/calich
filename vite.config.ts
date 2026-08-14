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
      "/api": "http://localhost:8080",
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
