import { defineConfig } from "vitest/config";

// Tests live outside src/ on purpose: they must never be picked up by the
// production build (go:embed) or by the i18n literal scanner.
export default defineConfig({
  test: {
    environment: "node",
    include: ["tests/**/*.test.ts"],
  },
});
