import { defineConfig } from "cypress";

// Explicit isolated live fixture only; no kubectl/token discovery.
export default defineConfig({
  e2e: {
    supportFile: false,
    specPattern: "cypress/e2e/celln-live.cy.ts",
    viewportWidth: 1280,
    viewportHeight: 1000,
    video: false,
  },
});
