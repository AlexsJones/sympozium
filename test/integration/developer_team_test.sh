#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# Integration test: Developer Team PersonaPack
#
# Simulates the 7-agent developer team working on containerising
# AlexsJones/labelynx.  Each function acts as one persona, using the
# exact same tools (gh CLI + git) that the real agents would use via
# the github-gitops / software-dev sidecar.
#
# Prerequisites:
#   - GITHUB_TOKEN set with repo scope
#   - gh CLI authenticated
# ---------------------------------------------------------------------------
set -euo pipefail

REPO="AlexsJones/labelynx"
DEFAULT_BRANCH="main"
WORK_DIR=$(mktemp -d)
LABEL_PREFIX="sympozium"
RUN_ID=$(date +%s)

# Colours for persona output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
WHITE='\033[0;37m'
NC='\033[0m'

log() {
  local persona="$1" colour="$2"; shift 2
  printf "${colour}[%-18s]${NC} %s\n" "$persona" "$*" >&2
}

# ---------------------------------------------------------------------------
# PERSONA 1: Tech Lead — create the tracking issue
# ---------------------------------------------------------------------------
tech_lead_create_issue() {
  log "Tech Lead" "$MAGENTA" "Creating tracking issue for containerisation..."

  local issue_url
  issue_url=$(gh issue create \
    --repo "$REPO" \
    --title "feat: containerise labelynx with Docker" \
    --body "$(cat <<'BODY'
## Objective

Containerise the labelynx Rust application so it can be built and run via Docker.

## Acceptance Criteria

- [ ] Multi-stage Dockerfile (builder + runtime) for minimal image size
- [ ] .dockerignore to exclude unnecessary files
- [ ] docker-compose.yml for local development
- [ ] GitHub Actions workflow to build and push the Docker image
- [ ] README updated with Docker usage instructions

## Team Assignments

- **backend-dev**: Create `Dockerfile` (multi-stage Rust build)
- **devops-engineer**: Create `docker-compose.yml` + CI workflow
- **docs-writer**: Update `README.md` with Docker instructions
- **qa-engineer**: Validate the Dockerfile builds correctly
- **code-reviewer**: Review all PRs

---
*Created by Sympozium Agent (tech-lead)*
BODY
)" \
    --label "$LABEL_PREFIX")

  ISSUE_NUMBER=$(echo "$issue_url" | grep -oP '\d+$')
  log "Tech Lead" "$MAGENTA" "Created issue #${ISSUE_NUMBER} — ${issue_url}"
  echo "$ISSUE_NUMBER"
}

# ---------------------------------------------------------------------------
# PERSONA 2: Backend Dev — create the Dockerfile
# ---------------------------------------------------------------------------
backend_dev_dockerfile() {
  local issue_number="$1"
  local branch="sympozium/backend/${issue_number}-dockerfile"

  log "Backend Dev" "$GREEN" "Picking up issue #${issue_number} — creating Dockerfile..."

  cd "$WORK_DIR"
  gh repo clone "$REPO" backend-work -- --quiet 2>/dev/null
  cd backend-work
  git checkout -b "$branch" "origin/${DEFAULT_BRANCH}"

  # Comment on the issue to claim it
  gh issue comment "$issue_number" --repo "$REPO" \
    --body "Claiming the Dockerfile task. Will create a multi-stage build.

*— Sympozium Agent (backend-dev)*"

  # Create .dockerignore
  cat > .dockerignore <<'EOF'
target/
.git/
.github/
*.md
.gitignore
.dockerignore
docker-compose.yml
EOF

  # Create Dockerfile
  cat > Dockerfile <<'EOF'
# ---------- builder ----------
FROM rust:1.87-slim AS builder

WORKDIR /app

# Cache dependency builds
COPY Cargo.toml Cargo.lock ./
RUN mkdir src && echo 'fn main() {}' > src/main.rs && \
    cargo build --release && \
    rm -rf src

# Build the real application
COPY src/ src/
RUN touch src/main.rs && cargo build --release

# ---------- runtime ----------
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/target/release/labelynx /usr/local/bin/labelynx

ENV RUST_LOG=info

ENTRYPOINT ["labelynx"]
EOF

  git add Dockerfile .dockerignore
  git commit -m "feat(docker): add multi-stage Dockerfile and .dockerignore

Multi-stage build using rust:1.87-slim for building and
debian:bookworm-slim for runtime. Caches dependency builds
for faster rebuilds.

Resolves #${issue_number}

Co-Authored-By: Sympozium Agent <sympozium@users.noreply.github.com>"

  git push origin "$branch" 2>/dev/null

  PR_URL=$(gh pr create \
    --repo "$REPO" \
    --base "$DEFAULT_BRANCH" \
    --head "$branch" \
    --title "feat(docker): add multi-stage Dockerfile" \
    --body "$(cat <<BODY
## Summary

Adds a multi-stage Dockerfile for building and running labelynx in a container.

## Changes

- **Dockerfile**: Multi-stage build (rust:1.87-slim builder → debian:bookworm-slim runtime)
  - Dependency caching layer for fast rebuilds
  - Minimal runtime image with only ca-certificates
- **.dockerignore**: Excludes target/, .git/, docs, and dev files

## Testing

\`\`\`bash
docker build -t labelynx .
docker run -e GITHUB_TOKEN=\$GITHUB_TOKEN -e OPENAI_API_KEY=\$OPENAI_API_KEY labelynx --repository owner/repo
\`\`\`

Resolves #${issue_number}

---
*Authored by Sympozium Agent (backend-dev)*
BODY
)" \
    --label "$LABEL_PREFIX")

  log "Backend Dev" "$GREEN" "Opened PR: ${PR_URL}"
  echo "$PR_URL"
}

# ---------------------------------------------------------------------------
# PERSONA 3: DevOps Engineer — docker-compose + CI workflow
# ---------------------------------------------------------------------------
devops_engineer_ci() {
  local issue_number="$1"
  local branch="sympozium/devops/${issue_number}-docker-ci"

  log "DevOps Engineer" "$CYAN" "Picking up issue #${issue_number} — creating docker-compose + CI..."

  cd "$WORK_DIR"
  gh repo clone "$REPO" devops-work -- --quiet 2>/dev/null
  cd devops-work
  git checkout -b "$branch" "origin/${DEFAULT_BRANCH}"

  gh issue comment "$issue_number" --repo "$REPO" \
    --body "Taking on docker-compose.yml and CI workflow for Docker builds.

*— Sympozium Agent (devops-engineer)*"

  # Create docker-compose.yml
  cat > docker-compose.yml <<'EOF'
services:
  labelynx:
    build: .
    environment:
      - GITHUB_TOKEN=${GITHUB_TOKEN}
      - OPENAI_API_KEY=${OPENAI_API_KEY}
      - RUST_LOG=${RUST_LOG:-info}
    command: ["--repository", "${TARGET_REPOSITORY:-owner/repo}"]
EOF

  # Create CI workflow
  mkdir -p .github/workflows
  cat > .github/workflows/docker.yml <<'EOF'
name: Docker Build

on:
  push:
    branches: [main]
    tags: ['v*.*.*']
  pull_request:
    branches: [main]

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository }}

jobs:
  build:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to Container Registry
        if: github.event_name != 'pull_request'
        uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract metadata
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          tags: |
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=sha,prefix=
            type=raw,value=latest,enable={{is_default_branch}}

      - name: Build and push
        uses: docker/build-push-action@v5
        with:
          context: .
          push: ${{ github.event_name != 'pull_request' }}
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
EOF

  git add docker-compose.yml .github/workflows/docker.yml
  git commit -m "ci(docker): add docker-compose and GitHub Actions workflow

- docker-compose.yml for local development with env var passthrough
- GitHub Actions workflow for building and pushing to ghcr.io
  - Builds on push to main and tags
  - Pushes to GHCR with semver + sha + latest tags
  - Uses Docker Buildx with GHA caching

Resolves #${issue_number}

Co-Authored-By: Sympozium Agent <sympozium@users.noreply.github.com>"

  git push origin "$branch" 2>/dev/null

  PR_URL=$(gh pr create \
    --repo "$REPO" \
    --base "$DEFAULT_BRANCH" \
    --head "$branch" \
    --title "ci(docker): add docker-compose and CI workflow" \
    --body "$(cat <<BODY
## Summary

Adds docker-compose for local dev and a GitHub Actions workflow for automated Docker image builds.

## Changes

- **docker-compose.yml**: Local development setup with env var passthrough for GITHUB_TOKEN, OPENAI_API_KEY, and TARGET_REPOSITORY
- **.github/workflows/docker.yml**: CI pipeline that:
  - Builds on push to main and version tags
  - Pushes to ghcr.io with semver, sha, and latest tags
  - Uses Docker Buildx with GitHub Actions cache

## Testing

\`\`\`bash
# Local build + run
TARGET_REPOSITORY=owner/repo docker compose up --build

# CI triggers automatically on push to main
\`\`\`

Resolves #${issue_number}

---
*Authored by Sympozium Agent (devops-engineer)*
BODY
)" \
    --label "$LABEL_PREFIX")

  log "DevOps Engineer" "$CYAN" "Opened PR: ${PR_URL}"
  echo "$PR_URL"
}

# ---------------------------------------------------------------------------
# PERSONA 4: Docs Writer — update README with Docker instructions
# ---------------------------------------------------------------------------
docs_writer_readme() {
  local issue_number="$1"
  local branch="sympozium/docs/${issue_number}-docker-docs"

  log "Docs Writer" "$WHITE" "Picking up issue #${issue_number} — updating README with Docker docs..."

  cd "$WORK_DIR"
  gh repo clone "$REPO" docs-work -- --quiet 2>/dev/null
  cd docs-work
  git checkout -b "$branch" "origin/${DEFAULT_BRANCH}"

  gh issue comment "$issue_number" --repo "$REPO" \
    --body "Adding Docker usage instructions to the README.

*— Sympozium Agent (docs-writer)*"

  # Append Docker section to README
  cat >> README.md <<'EOF'

## Docker

### Quick Start

```bash
docker build -t labelynx .
docker run \
  -e GITHUB_TOKEN=your_token \
  -e OPENAI_API_KEY=your_key \
  labelynx --repository owner/repo
```

### Docker Compose

For local development, use docker-compose:

```bash
export GITHUB_TOKEN=your_token
export OPENAI_API_KEY=your_key
export TARGET_REPOSITORY=owner/repo
docker compose up --build
```

### Pre-built Images

Pull the latest image from GitHub Container Registry:

```bash
docker pull ghcr.io/alexsjones/labelynx:latest
docker run \
  -e GITHUB_TOKEN=your_token \
  -e OPENAI_API_KEY=your_key \
  ghcr.io/alexsjones/labelynx:latest --repository owner/repo
```

### Dry Run Mode

```bash
docker run \
  -e GITHUB_TOKEN=your_token \
  -e OPENAI_API_KEY=your_key \
  labelynx --repository owner/repo --dry-run
```
EOF

  git add README.md
  git commit -m "docs: add Docker usage instructions to README

Adds quick start, docker-compose, pre-built image pull, and
dry-run examples for running labelynx in containers.

Resolves #${issue_number}

Co-Authored-By: Sympozium Agent <sympozium@users.noreply.github.com>"

  git push origin "$branch" 2>/dev/null

  PR_URL=$(gh pr create \
    --repo "$REPO" \
    --base "$DEFAULT_BRANCH" \
    --head "$branch" \
    --title "docs: add Docker usage instructions" \
    --body "$(cat <<BODY
## Summary

Adds Docker documentation to the README covering all containerised usage patterns.

## Changes

- **README.md**: Added Docker section with:
  - Quick start (build + run)
  - Docker Compose local development
  - Pre-built image pull from ghcr.io
  - Dry run mode example

Resolves #${issue_number}

---
*Authored by Sympozium Agent (docs-writer)*
BODY
)" \
    --label "$LABEL_PREFIX")

  log "Docs Writer" "$WHITE" "Opened PR: ${PR_URL}"
  echo "$PR_URL"
}

# ---------------------------------------------------------------------------
# PERSONA 5: Code Reviewer — review the Dockerfile PR
# ---------------------------------------------------------------------------
code_reviewer_review() {
  local pr_number="$1"
  local pr_type="$2"

  log "Code Reviewer" "$YELLOW" "Reviewing PR #${pr_number} (${pr_type})..."

  # Read the diff
  local diff
  diff=$(gh pr diff "$pr_number" --repo "$REPO" 2>/dev/null || echo "(no diff)")

  case "$pr_type" in
    dockerfile)
      gh pr review "$pr_number" --repo "$REPO" --comment \
        --body "**Review: LGTM**

The Dockerfile follows best practices:
- Multi-stage build minimises image size
- Dependency caching layer speeds up rebuilds
- Runtime uses slim base with only ca-certificates
- .dockerignore properly excludes build artifacts and dev files

One minor suggestion for future improvement: consider using \`distroless\` as the runtime base for an even smaller attack surface. Non-blocking.

*— Sympozium Agent (code-reviewer)*"
      log "Code Reviewer" "$YELLOW" "Reviewed PR #${pr_number} — LGTM"
      ;;
    ci)
      gh pr review "$pr_number" --repo "$REPO" --comment \
        --body "**Review: LGTM**

CI workflow and docker-compose look solid:
- Buildx with GHA caching is the right approach
- Semver + SHA tagging covers all release patterns
- docker-compose correctly passes through required env vars
- GHCR login properly gated behind \`github.event_name != 'pull_request'\`

*— Sympozium Agent (code-reviewer)*"
      log "Code Reviewer" "$YELLOW" "Reviewed PR #${pr_number} — LGTM"
      ;;
    docs)
      gh pr review "$pr_number" --repo "$REPO" --comment \
        --body "**Review: LGTM**

Documentation covers all the key scenarios:
- Build from source
- Docker Compose for local dev
- Pre-built images from GHCR
- Dry-run mode

Clear and concise. Good to merge.

*— Sympozium Agent (code-reviewer)*"
      log "Code Reviewer" "$YELLOW" "Reviewed PR #${pr_number} — LGTM"
      ;;
  esac
}

# ---------------------------------------------------------------------------
# PERSONA 6: QA Engineer — validate the Dockerfile
# ---------------------------------------------------------------------------
qa_engineer_validate() {
  local issue_number="$1"
  local dockerfile_pr="$2"

  log "QA Engineer" "$RED" "Validating Dockerfile from PR #${dockerfile_pr}..."

  # Check the PR for common Dockerfile issues
  local diff
  diff=$(gh pr diff "$dockerfile_pr" --repo "$REPO" 2>/dev/null || echo "")

  # Create a QA comment on the Dockerfile PR
  gh pr comment "$dockerfile_pr" --repo "$REPO" \
    --body "$(cat <<BODY
## QA Validation Report

### Dockerfile Analysis

| Check | Status | Notes |
|-------|--------|-------|
| Multi-stage build | PASS | Builder and runtime stages properly separated |
| Base image pinned | PASS | rust:1.87-slim and debian:bookworm-slim are version-pinned |
| Dependency caching | PASS | Cargo.toml/Cargo.lock copied first for layer caching |
| Minimal runtime | PASS | Only ca-certificates installed in runtime |
| Non-root user | INFO | Consider adding a non-root USER directive for production |
| .dockerignore | PASS | Excludes target/, .git/, and dev files |
| ENTRYPOINT vs CMD | PASS | ENTRYPOINT used correctly for CLI tool |

### Recommendations (non-blocking)

1. Add \`USER 1000\` in the runtime stage for least-privilege
2. Add \`LABEL\` directives for OCI image metadata
3. Consider \`--mount=type=cache\` for cargo registry caching in CI

### Verdict: **PASS** — ready for merge

*— Sympozium Agent (qa-engineer)*
BODY
)"

  log "QA Engineer" "$RED" "QA validation complete for PR #${dockerfile_pr}"
}

# ---------------------------------------------------------------------------
# PERSONA 7: Tech Lead — final coordination
# ---------------------------------------------------------------------------
tech_lead_coordinate() {
  local issue_number="$1"
  shift
  local pr_urls=("$@")

  log "Tech Lead" "$MAGENTA" "Coordinating final status on issue #${issue_number}..."

  gh issue comment "$issue_number" --repo "$REPO" \
    --body "$(cat <<BODY
## Status Update — Containerisation Complete

All tasks have been completed by the team:

| Task | Status | PR |
|------|--------|----|
| Dockerfile (multi-stage) | PR opened + reviewed + QA passed | ${pr_urls[0]:-n/a} |
| docker-compose.yml + CI | PR opened + reviewed | ${pr_urls[1]:-n/a} |
| README Docker docs | PR opened + reviewed | ${pr_urls[2]:-n/a} |

### Next Steps
- Merge approved PRs
- Tag a release to trigger the Docker CI pipeline
- Verify the image is published to ghcr.io

*— Sympozium Agent (tech-lead)*
BODY
)"

  log "Tech Lead" "$MAGENTA" "Final status update posted to issue #${issue_number}"
}

# ---------------------------------------------------------------------------
# MAIN — orchestrate the team
# ---------------------------------------------------------------------------
main() {
  echo "" >&2
  echo "============================================================" >&2
  echo "  Sympozium Developer Team — Integration Test" >&2
  echo "  Target: ${REPO}" >&2
  echo "  Objective: Containerise with Docker" >&2
  echo "  Run ID: ${RUN_ID}" >&2
  echo "============================================================" >&2
  echo "" >&2

  # Ensure the sympozium label exists
  gh label create "$LABEL_PREFIX" --repo "$REPO" --description "Sympozium agent work" --color "6f42c1" 2>/dev/null || true

  # Phase 1: Tech Lead creates the tracking issue
  ISSUE_NUMBER=$(tech_lead_create_issue)
  echo "" >&2

  # Phase 2: Developers work in parallel (simulated sequentially here)
  DOCKERFILE_PR=$(backend_dev_dockerfile "$ISSUE_NUMBER" | tail -1)
  DOCKERFILE_PR_NUM=$(echo "$DOCKERFILE_PR" | grep -oP '\d+$')
  echo "" >&2

  CI_PR=$(devops_engineer_ci "$ISSUE_NUMBER" | tail -1)
  CI_PR_NUM=$(echo "$CI_PR" | grep -oP '\d+$')
  echo "" >&2

  DOCS_PR=$(docs_writer_readme "$ISSUE_NUMBER" | tail -1)
  DOCS_PR_NUM=$(echo "$DOCS_PR" | grep -oP '\d+$')
  echo "" >&2

  # Phase 3: Code Reviewer reviews all PRs
  code_reviewer_review "$DOCKERFILE_PR_NUM" "dockerfile"
  code_reviewer_review "$CI_PR_NUM" "ci"
  code_reviewer_review "$DOCS_PR_NUM" "docs"
  echo "" >&2

  # Phase 4: QA Engineer validates the Dockerfile
  qa_engineer_validate "$ISSUE_NUMBER" "$DOCKERFILE_PR_NUM"
  echo "" >&2

  # Phase 5: Tech Lead coordinates and summarises
  tech_lead_coordinate "$ISSUE_NUMBER" "$DOCKERFILE_PR" "$CI_PR" "$DOCS_PR"
  echo "" >&2

  echo "============================================================" >&2
  echo "  Test Complete!" >&2
  echo "" >&2
  echo "  Issue:  https://github.com/${REPO}/issues/${ISSUE_NUMBER}" >&2
  echo "  PRs:" >&2
  echo "    Dockerfile:     ${DOCKERFILE_PR}" >&2
  echo "    CI + Compose:   ${CI_PR}" >&2
  echo "    Docs:           ${DOCS_PR}" >&2
  echo "============================================================" >&2

  # Cleanup
  rm -rf "$WORK_DIR"
}

main "$@"
