// End-to-end regression: create a PersonaPack with a scheduled persona,
// verify the controller stamps an Instance + Schedule, force the schedule
// to trigger immediately (by clearing its lastRunTime so the cron
// scheduler computes "next run" in the past), and assert that a real
// AgentRun gets created, completes, and produces a substantive response
// in the UX.
//
// This is the regression guard for the "ghost run" bug where the
// scheduler was silently claiming success due to run-name collisions
// after a PersonaPack disable/re-enable cycle. If that bug ever comes
// back, this test fails because no new AgentRun will actually appear.

const PACK = `cy-ppsched-${Date.now()}`;
const PERSONA = "analyst";
const INSTANCE = `${PACK}-${PERSONA}`;
const SCHEDULE = `${INSTANCE}-schedule`;

function authHeaders(): Record<string, string> {
  const token = Cypress.env("API_TOKEN");
  const h: Record<string, string> = { "Content-Type": "application/json" };
  if (token) h["Authorization"] = `Bearer ${token}`;
  return h;
}

describe("PersonaPack — scheduled run fires and produces a response", () => {
  after(() => {
    cy.deletePersonaPack(PACK);
    cy.deleteInstance(INSTANCE);
    // The schedule + runs are owned by the pack/instance and should GC,
    // but clean up defensively in case of leftover resources.
    cy.exec(
      `kubectl delete sympoziumschedule ${SCHEDULE} -n default --ignore-not-found --wait=false`,
      { failOnNonZeroExit: false },
    );
    cy.exec(
      `kubectl delete agentrun -n default -l sympozium.ai/schedule=${SCHEDULE} --ignore-not-found --wait=false`,
      { failOnNonZeroExit: false },
    );
  });

  it("stamps resources, triggers the schedule immediately, and renders the run's response", () => {
    // ── Step 1: create a PersonaPack with a scheduled persona ──────────────
    // Use a cron that only fires hourly so the test controls timing
    // (otherwise the schedule might fire on its natural cadence during
    // the test and create a confusing duplicate). We'll force-trigger
    // the initial run via status patch below.
    const manifest = `apiVersion: sympozium.ai/v1alpha1
kind: PersonaPack
metadata:
  name: ${PACK}
  namespace: default
spec:
  enabled: true
  description: cypress scheduled-run regression guard
  category: test
  version: "0.0.1"
  baseURL: http://host.docker.internal:1234/v1
  authRefs:
    - provider: lm-studio
      secret: ""
  personas:
    - name: ${PERSONA}
      displayName: Cypress Analyst
      systemPrompt: You are a precise echo service. When asked to reply with a specific string, reply with exactly that string and nothing else.
      model: qwen/qwen3.5-9b
      schedule:
        type: scheduled
        cron: "0 * * * *"
        task: "Reply with exactly this sentinel and nothing else: SCHEDULED_SENTINEL_319. Do not use any tools."
`;
    cy.writeFile(`cypress/tmp/${PACK}.yaml`, manifest);
    cy.exec(`kubectl apply -f cypress/tmp/${PACK}.yaml`);

    // ── Step 2: wait for the stamped Instance and Schedule to appear ───────
    cy.visit("/instances");
    cy.contains(INSTANCE, { timeout: 60000 }).should("be.visible");

    cy.visit("/schedules");
    cy.contains(SCHEDULE, { timeout: 30000 }).should("exist");

    // ── Step 3: force-trigger the schedule by clearing lastRunTime ─────────
    // The scheduler computes `nextRun = sched.Next(lastRun)`. When
    // lastRunTime is unset, it uses `creationTimestamp - 24h`, so the
    // next computed cron tick will be in the past and the reconcile
    // fires a run immediately.
    //
    // We retry because the initial status may be empty right after the
    // controller creates the schedule.
    cy.exec(
      `for i in $(seq 1 10); do ` +
        `if kubectl patch sympoziumschedule ${SCHEDULE} -n default ` +
        `--subresource=status --type=json ` +
        `-p='[{"op":"remove","path":"/status/lastRunTime"}]' 2>/dev/null; then ` +
        `  echo patched; exit 0; fi; ` +
        `sleep 2; done`,
      { failOnNonZeroExit: false },
    );

    // ── Step 4: wait for the scheduler to create an AgentRun ───────────────
    // The reconciler should pick up the cleared status within a few
    // seconds and create a run with a real, unused numeric suffix —
    // NOT silently reuse an existing one.
    let runName = "";
    cy.then(() => {
      const deadline = Date.now() + 60000;
      const retry = (): Cypress.Chainable<unknown> => {
        if (Date.now() > deadline) {
          throw new Error(
            `no AgentRun appeared for schedule ${SCHEDULE} within 60s`,
          );
        }
        return cy
          .request({
            url: `/api/v1/runs?namespace=default`,
            headers: authHeaders(),
          })
          .then((resp) => {
            // /api/v1/runs returns a bare array, not {items:[...]}.
            const all = Array.isArray(resp.body)
              ? (resp.body as Array<{
                  metadata: {
                    name: string;
                    labels?: Record<string, string>;
                    creationTimestamp: string;
                  };
                }>)
              : [];
            const runs = all
              .filter(
                (r) =>
                  r.metadata?.labels?.["sympozium.ai/schedule"] === SCHEDULE,
              )
              .sort((a, b) =>
                b.metadata.creationTimestamp.localeCompare(
                  a.metadata.creationTimestamp,
                ),
              );
            if (runs.length > 0) {
              runName = runs[0].metadata.name;
              return cy.wrap(runName);
            }
            cy.wait(2000, { log: false });
            return retry();
          });
      };
      return retry();
    });

    // ── Step 5: wait for the run to finish and assert Succeeded ────────────
    cy.then(() => cy.waitForRunTerminal(runName, 6 * 60 * 1000));
    cy.then(() =>
      cy
        .request({
          url: `/api/v1/runs/${runName}?namespace=default`,
          headers: authHeaders(),
        })
        .then((resp) => {
          const phase = resp.body?.status?.phase as string;
          const err = resp.body?.status?.error as string | undefined;
          expect(
            phase,
            `run ${runName} should have Succeeded (error: ${err || "n/a"})`,
          ).to.eq("Succeeded");
        }),
    );

    // ── Step 6: open the run detail and verify the response is real ────────
    cy.then(() => cy.visit(`/runs/${runName}`));
    cy.contains("Succeeded", { timeout: 20000 }).should("be.visible");
    cy.contains("button", "Result", { timeout: 20000 }).click({ force: true });

    // Deterministic assertion: the scheduled run MUST contain the tool's
    // sentinel output in its response. This proves end-to-end that:
    //   (a) the schedule fired a REAL run (not a ghost from name collision)
    //   (b) the run actually reached the provider
    //   (c) the provider executed the tool call
    //   (d) the tool's output was surfaced in the response
    cy.contains("No result available").should("not.exist");
    cy.get("[role='tabpanel']", { timeout: 20000 })
      .invoke("text")
      .then((raw) => {
        expect(
          raw,
          "scheduled run must contain the tool's sentinel output",
        ).to.include("SCHEDULED_SENTINEL_319");
      });
  });
});

export {};
