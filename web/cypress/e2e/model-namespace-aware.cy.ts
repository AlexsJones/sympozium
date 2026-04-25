// Test: Namespace-aware model deployment and cross-namespace resolution.
//
// Verifies:
// - Models can be deployed to a custom namespace via the UI
// - The namespace column appears in the models table
// - The model detail page shows the namespace
// - The API respects namespace params on get/delete
// - Models in sympozium-system are the default (backward compat)
//
// Does NOT require a GPU or model download — tests are scoped to API + UI
// behavior, not full inference lifecycle.

const NS_MODEL_NAME = `cypress-ns-model-${Date.now()}`;
const DEFAULT_NS = "sympozium-system";
const CUSTOM_NS = "default"; // use 'default' since it always exists in Kind

function authHeaders(): Record<string, string> {
  const token = Cypress.env("API_TOKEN");
  const h: Record<string, string> = { "Content-Type": "application/json" };
  if (token) h["Authorization"] = `Bearer ${token}`;
  return h;
}

describe("Model Namespace Awareness", () => {
  afterEach(() => {
    // Clean up any models created during tests
    cy.request({
      method: "DELETE",
      url: `/api/v1/models/${NS_MODEL_NAME}?namespace=${DEFAULT_NS}`,
      headers: authHeaders(),
      failOnStatusCode: false,
    });
    cy.request({
      method: "DELETE",
      url: `/api/v1/models/${NS_MODEL_NAME}?namespace=${CUSTOM_NS}`,
      headers: authHeaders(),
      failOnStatusCode: false,
    });
  });

  it("creates a model in the default sympozium-system namespace via API", () => {
    cy.request({
      method: "POST",
      url: "/api/v1/models",
      headers: authHeaders(),
      body: {
        name: NS_MODEL_NAME,
        url: "https://example.com/fake-model.gguf",
        storageSize: "1Gi",
        memory: "1Gi",
        cpu: "1",
        gpu: 0,
        // no namespace field — should default to sympozium-system
      },
    }).then((resp) => {
      expect(resp.status).to.equal(201);
      expect(resp.body.metadata.namespace).to.equal(DEFAULT_NS);
    });
  });

  it("creates a model in a custom namespace via API", () => {
    cy.request({
      method: "POST",
      url: "/api/v1/models",
      headers: authHeaders(),
      body: {
        name: NS_MODEL_NAME,
        url: "https://example.com/fake-model.gguf",
        storageSize: "1Gi",
        memory: "1Gi",
        cpu: "1",
        gpu: 0,
        namespace: CUSTOM_NS,
      },
    }).then((resp) => {
      expect(resp.status).to.equal(201);
      expect(resp.body.metadata.namespace).to.equal(CUSTOM_NS);
    });
  });

  it("gets a model by namespace via API", () => {
    // Create in custom namespace first
    cy.request({
      method: "POST",
      url: "/api/v1/models",
      headers: authHeaders(),
      body: {
        name: NS_MODEL_NAME,
        url: "https://example.com/fake-model.gguf",
        storageSize: "1Gi",
        memory: "1Gi",
        cpu: "1",
        namespace: CUSTOM_NS,
      },
    });

    // Get with correct namespace — should succeed
    cy.request({
      url: `/api/v1/models/${NS_MODEL_NAME}?namespace=${CUSTOM_NS}`,
      headers: authHeaders(),
    }).then((resp) => {
      expect(resp.status).to.equal(200);
      expect(resp.body.metadata.name).to.equal(NS_MODEL_NAME);
      expect(resp.body.metadata.namespace).to.equal(CUSTOM_NS);
    });

    // Get with wrong namespace — should 404
    cy.request({
      url: `/api/v1/models/${NS_MODEL_NAME}?namespace=${DEFAULT_NS}`,
      headers: authHeaders(),
      failOnStatusCode: false,
    }).then((resp) => {
      expect(resp.status).to.equal(404);
    });
  });

  it("lists models filtered by namespace", () => {
    // Create in custom namespace
    cy.request({
      method: "POST",
      url: "/api/v1/models",
      headers: authHeaders(),
      body: {
        name: NS_MODEL_NAME,
        url: "https://example.com/fake-model.gguf",
        storageSize: "1Gi",
        memory: "1Gi",
        cpu: "1",
        namespace: CUSTOM_NS,
      },
    });

    // List with namespace filter — should contain our model
    cy.request({
      url: `/api/v1/models?namespace=${CUSTOM_NS}`,
      headers: authHeaders(),
    }).then((resp) => {
      expect(resp.status).to.equal(200);
      const names = resp.body.map((m: { metadata: { name: string } }) => m.metadata.name);
      expect(names).to.include(NS_MODEL_NAME);
    });

    // List with default namespace filter — should NOT contain it
    cy.request({
      url: `/api/v1/models?namespace=${DEFAULT_NS}`,
      headers: authHeaders(),
    }).then((resp) => {
      expect(resp.status).to.equal(200);
      const names = resp.body.map((m: { metadata: { name: string } }) => m.metadata.name);
      expect(names).to.not.include(NS_MODEL_NAME);
    });

    // List without namespace filter — should contain all (including our model)
    cy.request({
      url: "/api/v1/models",
      headers: authHeaders(),
    }).then((resp) => {
      expect(resp.status).to.equal(200);
      const names = resp.body.map((m: { metadata: { name: string } }) => m.metadata.name);
      expect(names).to.include(NS_MODEL_NAME);
    });
  });

  it("deletes a model by namespace via API", () => {
    // Create in custom namespace
    cy.request({
      method: "POST",
      url: "/api/v1/models",
      headers: authHeaders(),
      body: {
        name: NS_MODEL_NAME,
        url: "https://example.com/fake-model.gguf",
        storageSize: "1Gi",
        memory: "1Gi",
        cpu: "1",
        namespace: CUSTOM_NS,
      },
    });

    // Delete with correct namespace — should succeed
    cy.request({
      method: "DELETE",
      url: `/api/v1/models/${NS_MODEL_NAME}?namespace=${CUSTOM_NS}`,
      headers: authHeaders(),
    }).then((resp) => {
      expect(resp.status).to.be.oneOf([200, 204]);
    });

    // Verify gone
    cy.request({
      url: `/api/v1/models/${NS_MODEL_NAME}?namespace=${CUSTOM_NS}`,
      headers: authHeaders(),
      failOnStatusCode: false,
    }).then((resp) => {
      expect(resp.status).to.equal(404);
    });
  });

  it("shows namespace column in the models UI", () => {
    // Create a model so the table renders
    cy.request({
      method: "POST",
      url: "/api/v1/models",
      headers: authHeaders(),
      body: {
        name: NS_MODEL_NAME,
        url: "https://example.com/fake-model.gguf",
        storageSize: "1Gi",
        memory: "1Gi",
        cpu: "1",
        namespace: CUSTOM_NS,
      },
    });

    cy.visit("/models");

    // Table should show Namespace column header
    cy.contains("th", "Namespace", { timeout: 15000 }).should("be.visible");

    // Our model row should show the namespace
    cy.contains(NS_MODEL_NAME, { timeout: 15000 })
      .closest("tr")
      .should("contain.text", CUSTOM_NS);
  });

  it("shows namespace on the model detail page", () => {
    cy.request({
      method: "POST",
      url: "/api/v1/models",
      headers: authHeaders(),
      body: {
        name: NS_MODEL_NAME,
        url: "https://example.com/fake-model.gguf",
        storageSize: "1Gi",
        memory: "1Gi",
        cpu: "1",
        namespace: CUSTOM_NS,
      },
    });

    cy.visit(`/models/${NS_MODEL_NAME}`);

    // Should show namespace in the status card
    cy.contains("Namespace", { timeout: 15000 }).should("be.visible");
    cy.contains(CUSTOM_NS).should("be.visible");
  });

  it("deploy dialog has namespace dropdown defaulting to sympozium-system", () => {
    cy.visit("/models");

    cy.contains("button", "Deploy Model", { timeout: 15000 }).click();
    cy.get("[role='dialog']").should("be.visible");

    // Namespace dropdown should show sympozium-system as default
    cy.get("[role='dialog']")
      .contains("label", "Namespace")
      .parent()
      .find("button[role='combobox']")
      .should("contain.text", "sympozium-system");

    // Close dialog
    cy.get("[role='dialog']")
      .contains("button", "Cancel")
      .click({ force: true });
  });

  it("deploy dialog allows changing namespace via dropdown", () => {
    cy.visit("/models");

    cy.contains("button", "Deploy Model", { timeout: 15000 }).click();
    cy.get("[role='dialog']").should("be.visible");

    const dialog = () => cy.get("[role='dialog']");

    // Fill required fields
    dialog().find("input").first().clear().type(NS_MODEL_NAME);
    dialog()
      .contains("label", "GGUF Download URL")
      .parent()
      .find("input")
      .clear()
      .type("https://example.com/fake-model.gguf", { delay: 0 });

    // Change namespace via dropdown
    dialog()
      .contains("label", "Namespace")
      .parent()
      .find("button[role='combobox']")
      .click();
    cy.get("[data-radix-popper-content-wrapper]", { timeout: 5000 })
      .contains(CUSTOM_NS)
      .click();

    // Deploy
    dialog()
      .contains("button", "Deploy")
      .should("not.be.disabled")
      .click({ force: true });

    // Dialog closes
    cy.get("[role='dialog']").should("not.exist", { timeout: 15000 });

    // Verify model was created in the custom namespace via API
    cy.request({
      url: `/api/v1/models/${NS_MODEL_NAME}?namespace=${CUSTOM_NS}`,
      headers: authHeaders(),
    }).then((resp) => {
      expect(resp.status).to.equal(200);
      expect(resp.body.metadata.namespace).to.equal(CUSTOM_NS);
    });
  });
});

export {};
