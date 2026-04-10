// Instance require-approval toggle: create an instance, enable approval via
// the detail page, verify the badge appears on the instances list, then
// disable and verify it disappears.

const INSTANCE = `cy-approval-toggle-${Date.now()}`;

describe("Instance -- require approval toggle", () => {
  before(() => {
    cy.createLMStudioInstance(INSTANCE);
  });

  after(() => {
    cy.deleteInstance(INSTANCE);
  });

  it("enables and disables approval via the detail page toggle", () => {
    // Instance list should NOT show gate badge initially.
    cy.visit("/instances");
    cy.contains("td", INSTANCE, { timeout: 20000 })
      .parents("tr")
      .within(() => {
        cy.get("[data-testid='instance-gate-badge']").should("not.exist");
      });

    // Visit detail page and enable the gate.
    cy.visit(`/instances/${INSTANCE}`);
    cy.get("[data-testid='gate-toggle-btn']", { timeout: 10000 })
      .should("contain.text", "Enable")
      .click();

    // Wait for the patch to complete.
    cy.contains("Instance updated", { timeout: 10000 }).should("be.visible");

    // Verify the toggle now shows "Disable".
    cy.get("[data-testid='gate-toggle-btn']", { timeout: 10000 }).should(
      "contain.text",
      "Disable",
    );

    // Instance list should now show the gate badge.
    cy.wait(2000);
    cy.visit("/instances");
    cy.contains("td", INSTANCE, { timeout: 20000 })
      .parents("tr")
      .within(() => {
        cy.get("[data-testid='instance-gate-badge']", { timeout: 10000 }).should("exist");
      });

    // Verify via API that the lifecycle has the manual gate hook.
    cy.request({
      url: `/api/v1/instances/${INSTANCE}?namespace=default`,
      headers: {
        "Content-Type": "application/json",
        ...(Cypress.env("API_TOKEN")
          ? { Authorization: `Bearer ${Cypress.env("API_TOKEN")}` }
          : {}),
      },
    }).then((resp) => {
      const hooks = resp.body.spec.agents?.default?.lifecycle?.postRun || [];
      const gateHook = hooks.find(
        (h: { name: string; gate?: boolean }) => h.name === "manual-approval-gate",
      );
      expect(gateHook).to.not.be.undefined;
      expect(gateHook.gate).to.eq(true);
    });

    // Now disable the gate.
    cy.visit(`/instances/${INSTANCE}`);
    cy.get("[data-testid='gate-toggle-btn']", { timeout: 10000 })
      .should("contain.text", "Disable")
      .click();
    cy.contains("Instance updated", { timeout: 10000 }).should("be.visible");
    cy.get("[data-testid='gate-toggle-btn']", { timeout: 10000 }).should(
      "contain.text",
      "Enable",
    );

    // Instance list should no longer show the gate badge.
    cy.wait(2000);
    cy.visit("/instances");
    cy.contains("td", INSTANCE, { timeout: 20000 })
      .parents("tr")
      .within(() => {
        cy.get("[data-testid='instance-gate-badge']").should("not.exist");
      });
  });
});

export {};
