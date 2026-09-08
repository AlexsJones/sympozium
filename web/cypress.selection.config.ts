import { defineConfig } from "cypress";

// UI contract fixtures only. No kubeconfig, cluster-token discovery, or model.
export default defineConfig({
  e2e: {
    baseUrl: "http://127.0.0.1:5178",
    env: { API_TOKEN: "public-selection-ui-fixture" },
    supportFile: "cypress/support/e2e.ts",
    specPattern: "cypress/e2e/celln-selection.cy.ts",
    viewportWidth: 1280,
    viewportHeight: 1000,
    video: false,
  },
});
