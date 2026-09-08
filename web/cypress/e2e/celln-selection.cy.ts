// Browser UI contract tests with intercepted APIs, not model/cluster E2E.
const native = { metadata: { name: "native", namespace: "default" }, spec: { celln: { contractVersion: "celln.json-tools/v1", revision: "v1", lifecycle: "disposable-one-shot", publisherKey: "public" } } };
const oci = { metadata: { name: "oci", namespace: "default" }, spec: { image: "example/oci:v1" } };
const tools = ["uppercase", "length"].map((name) => ({ metadata: { name, namespace: "default" }, spec: { revision: "v1", description: `${name} fixture`, publisherKey: "public-publisher", invocationABI: "celln.json-stdio/v1", lane: "tool", limits: { timeoutMillis: 1000, memoryBytes: 1024, argumentBytes: 128, outputBytes: 128, workspace: "none", effects: "none" } } }));

function openForm(runtime = "native", unavailable = false) {
  cy.intercept("GET", "/api/v1/**", { body: [] });
  cy.intercept("GET", "/api/v1/runs*", { body: [] });
  cy.intercept("GET", "/api/v1/agents*", { body: [{ metadata: { name: "agent" }, spec: { runtimeRef: runtime, agents: { default: { model: "deepseek-chat" } } } }] });
  cy.intercept("GET", "/api/v1/runtimes*", { body: [native, oci] });
  cy.intercept("GET", "/api/v1/celln-tools*", unavailable ? { statusCode: 503, body: "unavailable" } : { body: tools });
  cy.intercept("GET", "/api/v1/capabilities*", { body: { celln: { available: true, reason: "Node preflight only" } } });
  cy.visit("/runs?create=1");
  cy.get('[role="dialog"]').find('[role="combobox"]').eq(0).click();
  cy.get('[role="option"]').contains("agent").click();
  cy.get('textarea').type("Uppercase celln, then measure its length");
  cy.get('[role="dialog"]').find('[role="combobox"]').eq(2).click();
  cy.get('[role="option"]').contains("Celln —").click();
}

describe("Harness in Celln run selection", () => {
  it("sends explicit ordered lending without an OCI runtime task override", () => {
    openForm();
    cy.contains("Selection readiness is not established").scrollIntoView().should("be.visible");
    cy.contains("Uses whatever AI provider").should("not.exist");
    cy.get('[data-testid="celln-harness-selection"]').contains("label", "uppercase@v1").find("input").check();
    cy.get('[data-testid="celln-harness-selection"]').contains("label", "length@v1").find("input").check();
    cy.intercept("POST", "/api/v1/runs*", (request) => {
      expect(request.body.backend).to.eq("celln");
      expect(request.body.provider).to.eq("deepseek");
      expect(request.body.model).to.eq("deepseek-chat");
      expect(request.body).not.to.have.property("runtimeRef");
      expect(request.body.cellnSelection).to.deep.eq({ toolRefs: [{ name: "uppercase", revision: "v1" }, { name: "length", revision: "v1" }] });
      request.reply({ statusCode: 201, body: { metadata: { name: "pending" } } });
    }).as("create");
    cy.contains("button", "Request catalogue run").click();
    cy.wait("@create");
  });

  it("keeps an empty list explicit and does not lend the whole catalogue", () => {
    openForm();
    cy.intercept("POST", "/api/v1/runs*", (request) => {
      expect(request.body.cellnSelection.toolRefs).to.deep.eq([]);
      request.reply({ statusCode: 201, body: { metadata: { name: "empty" } } });
    }).as("empty");
    cy.contains("button", "Request catalogue run").click();
    cy.wait("@empty");
  });

  it("blocks an OCI-only runtime instead of silently changing execution", () => {
    openForm("oci");
    cy.get('[role="dialog"]').contains('[role="alert"]', "does not declare the supported native JSON Celln contract").scrollIntoView().should("be.visible");
    cy.contains("button", "Request catalogue run").should("be.disabled");
  });

  it("blocks when catalogue loading fails", () => {
    openForm("native", true);
    cy.contains("Cannot load the catalogue", { timeout: 15000 }).scrollIntoView().should("be.visible");
    cy.contains("button", "Request catalogue run").should("be.disabled");
  });

  it("sends a one-run override inside catalogue selection, never as an OCI task", () => {
    openForm("oci");
    cy.get('[role="dialog"]').find('[role="combobox"]').eq(1).click();
    cy.get('[role="option"]').contains("native").click();
    cy.intercept("POST", "/api/v1/runs*", (request) => {
      expect(request.body).not.to.have.property("runtimeRef");
      expect(request.body.cellnSelection).to.deep.eq({ runtimeRef: "native", toolRefs: [] });
      request.reply({ statusCode: 201, body: { metadata: { name: "override" } } });
    }).as("override");
    cy.contains("button", "Request catalogue run").click();
    cy.wait("@override");
  });
});
