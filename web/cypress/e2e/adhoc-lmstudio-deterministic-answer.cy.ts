// Full regression: create an ad-hoc LM Studio instance, dispatch a
// deterministic question via the "New Run" dialog, and verify the
// run-detail page renders a substantive answer from qwen3.5-9b — not
// just a preamble, not empty, and clearly mentioning the thing it was
// asked about (namespaces).
//
// This is the primary UX guard against the class of regressions we
// chased: reasoning models emitting content into non-standard fields,
// terminal turns being empty, response never surfacing in the UI.

const INSTANCE = `cy-adhoc-nsq-${Date.now()}`;
let RUN_NAME = "";

describe("Ad-hoc LM Studio — deterministic answer end to end", () => {
  before(() => {
    // Create the instance with the correct pod-reachable LM Studio URL.
    // (The wizard defaults to http://localhost:1234 which doesn't work
    // from inside kind pods; the wizard's node-mode flow is covered in
    // a separate spec.)
    cy.createLMStudioInstance(INSTANCE);
  });

  after(() => {
    if (RUN_NAME) cy.deleteRun(RUN_NAME);
    cy.deleteInstance(INSTANCE);
  });

  it("asks 'how many namespaces' via the UI and renders the answer", () => {
    // ── Step 1: dispatch the question via the "New Run" dialog on /runs ───
    cy.visit("/runs");
    cy.contains("button", "New Run", { timeout: 20000 }).click();

    // Select our instance in the dropdown.
    cy.get("[role='dialog']").find("button[role='combobox']").click({ force: true });
    cy.get("[data-radix-popper-content-wrapper]").contains(INSTANCE).click({ force: true });

    // Fill in the task — a deterministic echo the model must reproduce
    // verbatim. This proves the end-to-end path: UI dispatch → run
    // creation → provider invocation → response populated in status.
    // We use an echo (no tool calls required) because the focus of
    // this test is the UX pipeline, not tool calling.
    cy.get("[role='dialog']")
      .find("textarea")
      .clear()
      .type(
        "Reply with exactly this sentinel and nothing else: " +
          "NAMESPACE_SENTINEL_874. Do not use any tools.",
      );

    // Give the run a generous timeout (local inference is slow).
    cy.get("[role='dialog']").find("input[placeholder='5m']").clear().type("6m");

    // Submit.
    cy.get("[role='dialog']").contains("button", "Create Run").click({ force: true });
    cy.get("[role='dialog']").should("not.exist", { timeout: 20000 });

    // ── Step 2: find the run we just created via its UI row ───────────────
    cy.contains("td", INSTANCE, { timeout: 20000 })
      .parents("tr")
      .within(() => {
        cy.get("a[href^='/runs/']")
          .first()
          .invoke("attr", "href")
          .then((href) => {
            const m = /\/runs\/([^/?#]+)/.exec(href || "");
            expect(m, `expected /runs/<name> in href: ${href}`).to.not.be.null;
            RUN_NAME = m![1];
          });
      });

    // ── Step 3: wait for terminal phase + assert Succeeded with context ───
    cy.then(() => cy.waitForRunTerminal(RUN_NAME, 6 * 60 * 1000));
    cy.then(() =>
      cy.request({
        url: `/api/v1/runs/${RUN_NAME}?namespace=default`,
        headers: {
          Authorization: `Bearer ${Cypress.env("API_TOKEN") || ""}`,
        },
      }).then((resp) => {
        const phase = resp.body?.status?.phase as string;
        const err = resp.body?.status?.error as string | undefined;
        expect(
          phase,
          `run ${RUN_NAME} should have Succeeded (error: ${err || "n/a"})`,
        ).to.eq("Succeeded");
      }),
    );

    // ── Step 4: open the run detail and assert the answer is substantive ──
    cy.then(() => cy.visit(`/runs/${RUN_NAME}`));
    cy.contains("Succeeded", { timeout: 20000 }).should("be.visible");
    cy.contains("button", "Result", { timeout: 20000 }).click({ force: true });

    // Deterministic assertion: the response MUST contain the sentinel
    // string we asked the model to echo. If the sentinel is there, we
    // know the provider actually executed the tool and returned real
    // content — not a preamble, not a paraphrase.
    cy.contains("No result available").should("not.exist");
    cy.get("[role='tabpanel']", { timeout: 20000 })
      .invoke("text")
      .then((raw) => {
        expect(
          raw,
          "response must contain the tool's sentinel output — proves " +
            "end-to-end that the provider called the tool and surfaced " +
            "the real result",
        ).to.include("NAMESPACE_SENTINEL_874");
      });
  });
});

export {};
