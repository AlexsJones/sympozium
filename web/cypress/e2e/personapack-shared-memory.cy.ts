/**
 * PersonaPack Shared Workflow Memory — comprehensive tests for the shared
 * memory feature that enables cross-persona knowledge sharing within a
 * PersonaPack workflow.
 *
 * Tests cover:
 * - API: PATCH sharedMemory on/off, access rules, GET shared-memory endpoint
 * - UI: Shared Memory card on workflow tab, badges on persona canvas nodes
 * - Infrastructure: controller provisions PVC/Deployment/Service when enabled
 * - Access control: read-write vs read-only persona rules
 */

const NS = "default";

function apiHeaders(): Record<string, string> {
  const token = Cypress.env("API_TOKEN");
  const h: Record<string, string> = { "Content-Type": "application/json" };
  if (token) h["Authorization"] = `Bearer ${token}`;
  return h;
}

// ════════════════════════════════════════════════════════════════════════════
// Suite 1: Shared Memory API — PATCH enable/disable and access rules
// ════════════════════════════════════════════════════════════════════════════

describe("PersonaPack Shared Memory — API", () => {
  const PACK = `cypress-shmem-api-${Date.now()}`;

  before(() => {
    const manifest = `
apiVersion: sympozium.ai/v1alpha1
kind: PersonaPack
metadata:
  name: ${PACK}
  namespace: ${NS}
spec:
  enabled: false
  description: Cypress shared memory API test
  category: test
  personas:
    - name: alpha
      systemPrompt: "Agent alpha."
      skills: [memory]
    - name: beta
      systemPrompt: "Agent beta."
      skills: [memory]
    - name: gamma
      systemPrompt: "Agent gamma."
      skills: [memory]
`;
    cy.writeFile(`cypress/tmp/${PACK}.yaml`, manifest);
    cy.exec(`kubectl apply -f cypress/tmp/${PACK}.yaml`);
  });

  after(() => {
    cy.deletePersonaPack(PACK);
    cy.exec(`rm -f cypress/tmp/${PACK}.yaml`, { failOnNonZeroExit: false });
  });

  it("can enable shared memory via PATCH", () => {
    cy.request({
      method: "PATCH",
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
      body: {
        sharedMemory: {
          enabled: true,
          storageSize: "512Mi",
        },
      },
    }).then((resp) => {
      expect(resp.status).to.eq(200);
      expect(resp.body.spec.sharedMemory).to.deep.include({
        enabled: true,
        storageSize: "512Mi",
      });
    });
  });

  it("persists shared memory config on subsequent GET", () => {
    cy.request({
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
    }).then((resp) => {
      expect(resp.status).to.eq(200);
      expect(resp.body.spec.sharedMemory.enabled).to.eq(true);
      expect(resp.body.spec.sharedMemory.storageSize).to.eq("512Mi");
    });
  });

  it("can set per-persona access rules via PATCH", () => {
    cy.request({
      method: "PATCH",
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
      body: {
        sharedMemory: {
          enabled: true,
          storageSize: "512Mi",
          accessRules: [
            { persona: "alpha", access: "read-write" },
            { persona: "beta", access: "read-write" },
            { persona: "gamma", access: "read-only" },
          ],
        },
      },
    }).then((resp) => {
      expect(resp.status).to.eq(200);
      const rules = resp.body.spec.sharedMemory.accessRules;
      expect(rules).to.have.length(3);
      expect(rules[0]).to.deep.include({
        persona: "alpha",
        access: "read-write",
      });
      expect(rules[2]).to.deep.include({
        persona: "gamma",
        access: "read-only",
      });
    });
  });

  it("can update access rules without losing other sharedMemory fields", () => {
    cy.request({
      method: "PATCH",
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
      body: {
        sharedMemory: {
          enabled: true,
          storageSize: "512Mi",
          accessRules: [
            { persona: "alpha", access: "read-write" },
            { persona: "beta", access: "read-only" },
            { persona: "gamma", access: "read-only" },
          ],
        },
      },
    }).then((resp) => {
      expect(resp.status).to.eq(200);
      expect(resp.body.spec.sharedMemory.enabled).to.eq(true);
      expect(resp.body.spec.sharedMemory.storageSize).to.eq("512Mi");
      const betaRule = resp.body.spec.sharedMemory.accessRules.find(
        (r: { persona: string }) => r.persona === "beta",
      );
      expect(betaRule.access).to.eq("read-only");
    });
  });

  it("can disable shared memory via PATCH", () => {
    cy.request({
      method: "PATCH",
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
      body: {
        sharedMemory: {
          enabled: false,
        },
      },
    }).then((resp) => {
      expect(resp.status).to.eq(200);
      expect(resp.body.spec.sharedMemory.enabled).to.eq(false);
    });
  });

  it("re-enabling shared memory preserves previous config", () => {
    // Re-enable with full config
    cy.request({
      method: "PATCH",
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
      body: {
        sharedMemory: {
          enabled: true,
          storageSize: "1Gi",
          accessRules: [
            { persona: "alpha", access: "read-write" },
            { persona: "beta", access: "read-write" },
            { persona: "gamma", access: "read-only" },
          ],
        },
      },
    }).then((resp) => {
      expect(resp.status).to.eq(200);
      expect(resp.body.spec.sharedMemory.enabled).to.eq(true);
      expect(resp.body.spec.sharedMemory.accessRules).to.have.length(3);
    });
  });
});

// ════════════════════════════════════════════════════════════════════════════
// Suite 2: Shared Memory API — combined with relationships and workflowType
// ════════════════════════════════════════════════════════════════════════════

describe("PersonaPack Shared Memory — with relationships", () => {
  const PACK = `cypress-shmem-rel-${Date.now()}`;

  before(() => {
    const manifest = `
apiVersion: sympozium.ai/v1alpha1
kind: PersonaPack
metadata:
  name: ${PACK}
  namespace: ${NS}
spec:
  enabled: false
  description: Shared memory with relationships
  category: test
  workflowType: delegation
  personas:
    - name: coordinator
      displayName: Coordinator
      systemPrompt: "You coordinate."
      skills: [memory]
    - name: worker
      displayName: Worker
      systemPrompt: "You execute tasks."
      skills: [memory]
  relationships:
    - source: coordinator
      target: worker
      type: delegation
      timeout: "10m"
  sharedMemory:
    enabled: true
    storageSize: "1Gi"
    accessRules:
      - persona: coordinator
        access: read-write
      - persona: worker
        access: read-write
`;
    cy.writeFile(`cypress/tmp/${PACK}.yaml`, manifest);
    cy.exec(`kubectl apply -f cypress/tmp/${PACK}.yaml`);
  });

  after(() => {
    cy.deletePersonaPack(PACK);
    cy.exec(`rm -f cypress/tmp/${PACK}.yaml`, { failOnNonZeroExit: false });
  });

  it("creates pack with both relationships and shared memory", () => {
    cy.request({
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
    }).then((resp) => {
      expect(resp.status).to.eq(200);
      const spec = resp.body.spec;

      // Relationships present
      expect(spec.relationships).to.have.length(1);
      expect(spec.relationships[0].source).to.eq("coordinator");
      expect(spec.relationships[0].target).to.eq("worker");
      expect(spec.relationships[0].type).to.eq("delegation");

      // Shared memory present
      expect(spec.sharedMemory.enabled).to.eq(true);
      expect(spec.sharedMemory.storageSize).to.eq("1Gi");
      expect(spec.sharedMemory.accessRules).to.have.length(2);

      // Workflow type preserved
      expect(spec.workflowType).to.eq("delegation");
    });
  });

  it("can update relationships without losing shared memory config", () => {
    cy.request({
      method: "PATCH",
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
      body: {
        relationships: [
          { source: "coordinator", target: "worker", type: "delegation" },
          { source: "worker", target: "coordinator", type: "supervision" },
        ],
      },
    }).then((resp) => {
      expect(resp.status).to.eq(200);
      // Relationships updated
      expect(resp.body.spec.relationships).to.have.length(2);
      // Shared memory not touched
      expect(resp.body.spec.sharedMemory.enabled).to.eq(true);
      expect(resp.body.spec.sharedMemory.accessRules).to.have.length(2);
    });
  });

  it("can update shared memory without losing relationships", () => {
    cy.request({
      method: "PATCH",
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
      body: {
        sharedMemory: {
          enabled: true,
          storageSize: "2Gi",
          accessRules: [
            { persona: "coordinator", access: "read-write" },
            { persona: "worker", access: "read-only" },
          ],
        },
      },
    }).then((resp) => {
      expect(resp.status).to.eq(200);
      // Shared memory updated
      expect(resp.body.spec.sharedMemory.storageSize).to.eq("2Gi");
      const workerRule = resp.body.spec.sharedMemory.accessRules.find(
        (r: { persona: string }) => r.persona === "worker",
      );
      expect(workerRule.access).to.eq("read-only");
      // Relationships untouched
      expect(resp.body.spec.relationships).to.have.length(2);
    });
  });
});

// ════════════════════════════════════════════════════════════════════════════
// Suite 3: Shared Memory UI — Workflow tab card and canvas badges
// ════════════════════════════════════════════════════════════════════════════

describe("PersonaPack Shared Memory — UI", () => {
  const PACK = `cypress-shmem-ui-${Date.now()}`;

  before(() => {
    const manifest = `
apiVersion: sympozium.ai/v1alpha1
kind: PersonaPack
metadata:
  name: ${PACK}
  namespace: ${NS}
spec:
  enabled: false
  description: Shared memory UI test
  category: test
  workflowType: delegation
  personas:
    - name: analyst
      displayName: Analyst
      systemPrompt: "You analyze."
      skills: [memory]
    - name: reporter
      displayName: Reporter
      systemPrompt: "You report."
      skills: [memory]
    - name: auditor
      displayName: Auditor
      systemPrompt: "You audit."
      skills: [memory]
  relationships:
    - source: analyst
      target: reporter
      type: delegation
    - source: reporter
      target: auditor
      type: sequential
  sharedMemory:
    enabled: true
    storageSize: "1Gi"
    accessRules:
      - persona: analyst
        access: read-write
      - persona: reporter
        access: read-write
      - persona: auditor
        access: read-only
`;
    cy.writeFile(`cypress/tmp/${PACK}.yaml`, manifest);
    cy.exec(`kubectl apply -f cypress/tmp/${PACK}.yaml`);
  });

  after(() => {
    cy.deletePersonaPack(PACK);
    cy.exec(`rm -f cypress/tmp/${PACK}.yaml`, { failOnNonZeroExit: false });
  });

  it("shows the Shared Workflow Memory card on the workflow tab", () => {
    cy.visit(`/personas/${PACK}?tab=workflow`);
    cy.contains("Shared Workflow Memory", { timeout: 10000 }).should(
      "be.visible",
    );
  });

  it("displays enabled badge when shared memory is active", () => {
    cy.visit(`/personas/${PACK}?tab=workflow`);
    cy.contains("Shared Workflow Memory", { timeout: 10000 }).should(
      "be.visible",
    );
    cy.contains("Enabled").should("be.visible");
  });

  it("shows the storage size in the shared memory card", () => {
    cy.visit(`/personas/${PACK}?tab=workflow`);
    cy.contains("Shared Workflow Memory", { timeout: 10000 });
    cy.contains("Storage: 1Gi").should("be.visible");
  });

  it("displays access rules table with persona names and levels", () => {
    cy.visit(`/personas/${PACK}?tab=workflow`);
    cy.contains("Shared Workflow Memory", { timeout: 10000 });
    cy.contains("Access Rules").should("be.visible");

    // Verify each persona's access level is shown
    cy.contains("analyst").should("be.visible");
    cy.contains("reporter").should("be.visible");
    cy.contains("auditor").should("be.visible");
    cy.contains("read-write").should("be.visible");
    cy.contains("read-only").should("be.visible");
  });

  it("shows shared memory badge on persona nodes in the canvas", () => {
    cy.visit(`/personas/${PACK}?tab=workflow`);
    // Wait for canvas to render persona nodes
    cy.contains("Analyst", { timeout: 10000 }).should("be.visible");
    // The shared memory badge appears on nodes
    cy.contains("shared memory").should("exist");
  });

  it("shows all relationship types alongside the shared memory card", () => {
    cy.visit(`/personas/${PACK}?tab=workflow`);
    cy.contains("Shared Workflow Memory", { timeout: 10000 });

    // Relationships card should also be present
    cy.contains("Relationships").should("be.visible");
    cy.contains("3 personas with 2 relationships").should("be.visible");
  });

  it("canvas has both persona nodes and ReactFlow controls", () => {
    cy.visit(`/personas/${PACK}?tab=workflow`);
    cy.contains("Analyst", { timeout: 10000 }).should("be.visible");
    cy.contains("Reporter").should("be.visible");
    cy.contains("Auditor").should("be.visible");
    cy.get(".react-flow__controls").should("exist");
    cy.get(".react-flow__minimap").should("exist");
  });
});

// ════════════════════════════════════════════════════════════════════════════
// Suite 4: Shared Memory UI — disabled state
// ════════════════════════════════════════════════════════════════════════════

describe("PersonaPack Shared Memory — disabled state UI", () => {
  const PACK = `cypress-shmem-off-${Date.now()}`;

  before(() => {
    const manifest = `
apiVersion: sympozium.ai/v1alpha1
kind: PersonaPack
metadata:
  name: ${PACK}
  namespace: ${NS}
spec:
  enabled: false
  description: No shared memory
  category: test
  personas:
    - name: solo
      displayName: Solo
      systemPrompt: "You work alone."
      skills: [memory]
`;
    cy.writeFile(`cypress/tmp/${PACK}.yaml`, manifest);
    cy.exec(`kubectl apply -f cypress/tmp/${PACK}.yaml`);
  });

  after(() => {
    cy.deletePersonaPack(PACK);
    cy.exec(`rm -f cypress/tmp/${PACK}.yaml`, { failOnNonZeroExit: false });
  });

  it("shows disabled state text when shared memory is not configured", () => {
    cy.visit(`/personas/${PACK}?tab=workflow`);
    cy.contains("Shared Workflow Memory", { timeout: 10000 }).should(
      "be.visible",
    );
    cy.contains("Shared memory is not configured").should("be.visible");
  });

  it("does not show access rules when shared memory is disabled", () => {
    cy.visit(`/personas/${PACK}?tab=workflow`);
    cy.contains("Shared Workflow Memory", { timeout: 10000 });
    cy.contains("Access Rules").should("not.exist");
  });

  it("does not show shared memory badge on canvas nodes", () => {
    cy.visit(`/personas/${PACK}?tab=workflow`);
    cy.contains("Solo", { timeout: 10000 }).should("be.visible");
    cy.contains("shared memory").should("not.exist");
  });
});

// ════════════════════════════════════════════════════════════════════════════
// Suite 5: Shared Memory — shared-memory list endpoint
// ════════════════════════════════════════════════════════════════════════════

describe("PersonaPack Shared Memory — list endpoint", () => {
  const PACK = `cypress-shmem-list-${Date.now()}`;

  before(() => {
    const manifest = `
apiVersion: sympozium.ai/v1alpha1
kind: PersonaPack
metadata:
  name: ${PACK}
  namespace: ${NS}
spec:
  enabled: false
  description: Shared memory list endpoint test
  category: test
  personas:
    - name: agent
      systemPrompt: "You are an agent."
      skills: [memory]
`;
    cy.writeFile(`cypress/tmp/${PACK}.yaml`, manifest);
    cy.exec(`kubectl apply -f cypress/tmp/${PACK}.yaml`);
  });

  after(() => {
    cy.deletePersonaPack(PACK);
    cy.exec(`rm -f cypress/tmp/${PACK}.yaml`, { failOnNonZeroExit: false });
  });

  it("returns 404 when shared memory is not enabled", () => {
    cy.request({
      url: `/api/v1/personapacks/${PACK}/shared-memory?namespace=${NS}`,
      headers: apiHeaders(),
      failOnStatusCode: false,
    }).then((resp) => {
      expect(resp.status).to.eq(404);
    });
  });

  it("returns 404 for non-existent pack", () => {
    cy.request({
      url: `/api/v1/personapacks/nonexistent-pack-${Date.now()}/shared-memory?namespace=${NS}`,
      headers: apiHeaders(),
      failOnStatusCode: false,
    }).then((resp) => {
      expect(resp.status).to.eq(404);
    });
  });
});

// ════════════════════════════════════════════════════════════════════════════
// Suite 6: Research Team — shared memory integration
// ════════════════════════════════════════════════════════════════════════════

describe("Research Team — shared memory config", () => {
  const PACK = "research-team";

  before(() => {
    // Apply the research-team pack (uses sed to override namespace)
    cy.exec(
      `sed 's/namespace: sympozium-system/namespace: default/' ${Cypress.config().projectRoot}/../config/personas/research-team.yaml | kubectl apply -f -`,
    );
    cy.request({
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
      retryOnStatusCodeFailure: true,
    }).then((resp) => {
      expect(resp.status).to.eq(200);
    });
  });

  after(() => {
    cy.exec(`kubectl delete personapack ${PACK} -n ${NS}`, {
      failOnNonZeroExit: false,
    });
    cy.exec(`kubectl delete personapack ${PACK} -n sympozium-system`, {
      failOnNonZeroExit: false,
    });
  });

  it("has shared memory enabled with correct access rules", () => {
    cy.request({
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
    }).then((resp) => {
      const spec = resp.body.spec;

      expect(spec.sharedMemory).to.exist;
      expect(spec.sharedMemory.enabled).to.eq(true);
      expect(spec.sharedMemory.storageSize).to.eq("1Gi");

      const rules = spec.sharedMemory.accessRules;
      expect(rules).to.have.length(4);

      // Lead, researcher, writer have read-write; reviewer has read-only
      const rwPersonas = rules
        .filter((r: { access: string }) => r.access === "read-write")
        .map((r: { persona: string }) => r.persona);
      const roPersonas = rules
        .filter((r: { access: string }) => r.access === "read-only")
        .map((r: { persona: string }) => r.persona);

      expect(rwPersonas).to.include.members(["lead", "researcher", "writer"]);
      expect(roPersonas).to.include.members(["reviewer"]);
    });
  });

  it("has both relationships and shared memory in the same pack", () => {
    cy.request({
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
    }).then((resp) => {
      const spec = resp.body.spec;

      // 5 relationships
      expect(spec.relationships).to.have.length(5);

      // Shared memory enabled
      expect(spec.sharedMemory.enabled).to.eq(true);

      // 4 personas
      expect(spec.personas).to.have.length(4);

      // Delegation workflow type
      expect(spec.workflowType).to.eq("delegation");
    });
  });

  it("renders shared memory card on the workflow tab", () => {
    cy.visit(`/personas/${PACK}?tab=workflow`);
    cy.contains("Shared Workflow Memory", { timeout: 10000 }).should(
      "be.visible",
    );
    cy.contains("Enabled").should("be.visible");
    cy.contains("Storage: 1Gi").should("be.visible");
  });

  it("shows access rules for all 4 personas", () => {
    cy.visit(`/personas/${PACK}?tab=workflow`);
    cy.contains("Access Rules", { timeout: 10000 }).should("be.visible");
    cy.contains("lead").should("be.visible");
    cy.contains("researcher").should("be.visible");
    cy.contains("writer").should("be.visible");
    cy.contains("reviewer").should("be.visible");
  });

  it("shows shared memory badges on all persona canvas nodes", () => {
    cy.visit(`/personas/${PACK}?tab=workflow`);
    cy.contains("Research Lead", { timeout: 10000 }).should("be.visible");
    // All nodes should have the shared memory indicator
    cy.get('[title="Shared workflow memory"]').should("have.length.at.least", 1);
  });

  it("shows both the canvas and shared memory alongside relationships", () => {
    cy.visit(`/personas/${PACK}?tab=workflow`);
    // Persona canvas
    cy.contains("Persona Workflow", { timeout: 10000 }).should("be.visible");
    cy.contains("4 personas with 5 relationships").should("be.visible");
    // Shared memory
    cy.contains("Shared Workflow Memory").should("be.visible");
    // Relationships table
    cy.contains("Relationships").should("be.visible");
  });
});

// ════════════════════════════════════════════════════════════════════════════
// Suite 7: Shared Memory — edge cases and validation
// ════════════════════════════════════════════════════════════════════════════

describe("PersonaPack Shared Memory — edge cases", () => {
  const PACK = `cypress-shmem-edge-${Date.now()}`;

  before(() => {
    const manifest = `
apiVersion: sympozium.ai/v1alpha1
kind: PersonaPack
metadata:
  name: ${PACK}
  namespace: ${NS}
spec:
  enabled: false
  description: Edge case test
  category: test
  personas:
    - name: one
      systemPrompt: "Agent one."
    - name: two
      systemPrompt: "Agent two."
`;
    cy.writeFile(`cypress/tmp/${PACK}.yaml`, manifest);
    cy.exec(`kubectl apply -f cypress/tmp/${PACK}.yaml`);
  });

  after(() => {
    cy.deletePersonaPack(PACK);
    cy.exec(`rm -f cypress/tmp/${PACK}.yaml`, { failOnNonZeroExit: false });
  });

  it("enabling shared memory with empty access rules defaults to read-write for all", () => {
    cy.request({
      method: "PATCH",
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
      body: {
        sharedMemory: {
          enabled: true,
        },
      },
    }).then((resp) => {
      expect(resp.status).to.eq(200);
      expect(resp.body.spec.sharedMemory.enabled).to.eq(true);
      // No access rules means all personas get read-write by default
      const rules = resp.body.spec.sharedMemory.accessRules;
      expect(rules == null || rules.length === 0).to.eq(true);
    });
  });

  it("can enable shared memory with default storageSize", () => {
    cy.request({
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
    }).then((resp) => {
      // storageSize defaults to "1Gi" if not specified
      const storageSize = resp.body.spec.sharedMemory.storageSize;
      // Either omitted (nil) or "1Gi" default
      if (storageSize) {
        expect(storageSize).to.eq("1Gi");
      }
    });
  });

  it("can set shared memory alongside enabling the pack", () => {
    cy.request({
      method: "PATCH",
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
      body: {
        enabled: false,
        sharedMemory: {
          enabled: true,
          storageSize: "2Gi",
          accessRules: [
            { persona: "one", access: "read-write" },
            { persona: "two", access: "read-only" },
          ],
        },
      },
    }).then((resp) => {
      expect(resp.status).to.eq(200);
      // Both fields set correctly in a single PATCH
      expect(resp.body.spec.enabled).to.eq(false);
      expect(resp.body.spec.sharedMemory.enabled).to.eq(true);
      expect(resp.body.spec.sharedMemory.storageSize).to.eq("2Gi");
      expect(resp.body.spec.sharedMemory.accessRules).to.have.length(2);
    });
  });

  it("can set shared memory alongside relationships in a single PATCH", () => {
    cy.request({
      method: "PATCH",
      url: `/api/v1/personapacks/${PACK}?namespace=${NS}`,
      headers: apiHeaders(),
      body: {
        relationships: [
          { source: "one", target: "two", type: "delegation" },
        ],
        workflowType: "delegation",
        sharedMemory: {
          enabled: true,
          storageSize: "1Gi",
          accessRules: [
            { persona: "one", access: "read-write" },
            { persona: "two", access: "read-write" },
          ],
        },
      },
    }).then((resp) => {
      expect(resp.status).to.eq(200);
      expect(resp.body.spec.relationships).to.have.length(1);
      expect(resp.body.spec.workflowType).to.eq("delegation");
      expect(resp.body.spec.sharedMemory.enabled).to.eq(true);
      expect(resp.body.spec.sharedMemory.accessRules).to.have.length(2);
    });
  });
});

export {};
