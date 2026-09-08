// No stubbed responses: create/delete through the UI, or inspect the real result.
describe("live Harness-in-Celln", () => {
  it("uses the actual API and renders the selected run", () => {
    const namespace = Cypress.env("PROOF_NAMESPACE");
    expect(namespace).to.match(/^celln-catalogue-proof-/);
    const run = Cypress.env("PROOF_RUN");
    const cancel = Cypress.env("PROOF_ACTION") === "cancel";
    if (cancel) expect(run).to.be.a("string").and.not.be.empty;
    cy.visit(cancel ? "/runs" : run ? `/runs/${run}` : "/runs?create=1", {
      onBeforeLoad(win) {
        win.localStorage.setItem("sympozium_token", "public-loopback-browser-fixture");
        win.localStorage.setItem("sympozium_namespace", namespace);
      },
    });
    if (cancel) {
      // Observe the actual authenticated DELETE; do not stub acceptance or
      // disappearance. The Go proof independently waits for finalization.
      cy.intercept("DELETE", `/api/v1/runs/${run}*`).as("deleted");
      cy.get(`a[href="/runs/${run}"]`, { timeout: 20000 })
        .closest("tr").find('button[title="Delete"]').click();
      cy.wait("@deleted").its("response.statusCode").should("eq", 204);
      cy.get(`a[href="/runs/${run}"]`, { timeout: 20000 }).should("not.exist");
      return;
    }
    if (run) {
      cy.contains("Succeeded", { timeout: 20000 }).should("be.visible");
      cy.contains('[role="tab"]', "Result").click();
      cy.contains("CELLN has length 5").should("be.visible");
      return;
    }
    cy.get('[role="dialog"]').find('[role="combobox"]').eq(0).click();
    cy.get('[role="option"]').contains("agent").click();
    cy.get("textarea").type(Cypress.env("PROOF_TASK"));
    cy.get('[role="dialog"]').find('[role="combobox"]').eq(2).click();
    cy.get('[role="option"]').contains("Celln —").click();
    for (const name of ["uppercase", "length"]) {
      cy.get('[data-testid="celln-harness-selection"]').contains("label", `${name}@`).find("input").check();
    }
    // Observe the real request/response; never reply with fixture data.
    cy.get('[data-testid="celln-permission-preview"]').contains("Shared cell memory ceiling:").scrollIntoView().should("be.visible");
    cy.get('[data-testid="celln-permission-preview"]').contains("uppercase@").should("be.visible");
    cy.get('[data-testid="celln-permission-preview"]').contains("length@").should("be.visible");
    cy.intercept("POST", "/api/v1/runs*").as("created");
    cy.contains("button", "Request catalogue run").click();
    cy.wait("@created").then(({ request, response }) => {
      expect(response?.statusCode).to.eq(201);
      expect(request.body.backend).to.eq("celln");
      expect(request.body.cellnSelection.toolRefs.map((r: { name: string }) => r.name)).to.deep.eq(["uppercase", "length"]);
      expect(response?.body.metadata.namespace).to.eq(namespace);
    });
  });
});
