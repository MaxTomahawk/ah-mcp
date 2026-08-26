# AH API Modernization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Repair the broken AH basket/shopping-list/favourites/cart/order-detail behaviour and ship a directly deployable GHCR image from the fork.

**Architecture:** Keep the existing authenticated `appie.Client`, but add current GraphQL basket/favourite helpers inside `tools`. Existing MCP tool names stay stable; only the implementations behind broken tools change. Deployment moves from downloading a release binary at container start to a repository-built GHCR image.

**Tech Stack:** Go 1.23, mcp-go v0.45.0, existing appie-go v0.0.12 client, GitHub Actions, Docker/GHCR.

**Spec:** `docs/superpowers/specs/2026-08-26-ah-api-modernization-design.md`

## Global Constraints

- Modify only `MaxTomahawk/ah-mcp`.
- Do not fork or alter appie-go.
- Preserve existing MCP tool names.
- Do not commit HARs or sensitive account/session data.
- Do not implement unverified checkout/order-creation mutations.
- Keep callback port 9876 and the external proxy/tunnel architecture unchanged.

---

### Task 1: Current AH GraphQL helpers and regression tests

**Files:**
- Create: `tools/ahgraphql.go`
- Create: `tools/ahgraphql_test.go`

**Interfaces:**
- Consumes: `DoGraphQL(ctx, query, variables, result)` from the existing authenticated client.
- Produces: basket/favourite models and helpers used by basket/order tool handlers.

- [ ] **Step 1: Write failing tests** for basket decoding, basket add/update payloads, favourites listing and favourite item-ID resolution.
- [ ] **Step 2: Run `go test ./tools` and verify RED** in CI.
- [ ] **Step 3: Implement minimal GraphQL helpers** for `basket`, `basketItemsAdd`, `basketItemsUpdate`, `favoriteListV2`, `favoriteListProductsAddV2`, and `favoriteListProductsDeleteV2`.
- [ ] **Step 4: Run `go test ./tools` and verify GREEN**.
- [ ] **Step 5: Commit** the helper layer.

### Task 2: Repair shopping-list and favourites MCP tools

**Files:**
- Modify: `tools/basket.go`
- Extend: `tools/ahgraphql_test.go`

**Interfaces:**
- Consumes: Task 1 GraphQL helpers.
- Produces: fixed `ah_get_shopping_list`, add/remove/clear list tools, favourite-list tools.

- [ ] **Step 1: Add failing handler/helper regression tests** for free-text display fallback, product removal, clear behaviour and favourite list lookup/removal.
- [ ] **Step 2: Verify RED**.
- [ ] **Step 3: Replace obsolete REST reads/writes** with current basket/favourite GraphQL operations where verified.
- [ ] **Step 4: Keep free-text add on the working authenticated API path and correctly read returned notes/descriptions.**
- [ ] **Step 5: Make `ah_shopping_list_to_order` state-aware** and stop falsely claiming an order exists when AH only has list items.
- [ ] **Step 6: Verify GREEN**.
- [ ] **Step 7: Commit**.

### Task 3: Repair cart semantics and order detail totals

**Files:**
- Modify: `tools/orders.go`
- Extend: `tools/ahgraphql.go`
- Extend: `tools/ahgraphql_test.go`

**Interfaces:**
- Consumes: current basket state and existing order APIs.
- Produces: cart read/update/remove/clear that works with both list-only baskets and real active orders; accurate order totals.

- [ ] **Step 1: Add failing tests** for list-only basket cart output, add/update/remove without active order, and total-price fallback.
- [ ] **Step 2: Verify RED**.
- [ ] **Step 3: Implement basket-backed cart reads and list-only mutations.**
- [ ] **Step 4: Preserve active-order mutation path when a real online order is present.**
- [ ] **Step 5: Add fulfilment total lookup fallback for `ah_get_order_details` when dependency total is zero.**
- [ ] **Step 6: Verify GREEN**.
- [ ] **Step 7: Commit**.

### Task 4: Container image and CI

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/container.yml`
- Modify: `.github/workflows/release.yml`

**Interfaces:**
- Produces: `ghcr.io/maxtomahawk/ah-mcp:latest` plus SHA tag and release binaries.

- [ ] **Step 1: Add CI workflow** running `go test ./...` and `go vet ./...` on PR/push.
- [ ] **Step 2: Add minimal multi-stage Dockerfile** with non-root runtime user and CA certificates.
- [ ] **Step 3: Add GHCR publish workflow** for `main` and version tags with `packages: write`.
- [ ] **Step 4: Ensure release workflow runs tests before publishing binaries.**
- [ ] **Step 5: Commit**.

### Task 5: Documentation and final verification

**Files:**
- Modify: `README.md`
- Modify: `TESTING.md`

**Interfaces:**
- Produces: deployment instructions and explicit known limitation that delivery/pickup order creation is not automated without a verified mutation.

- [ ] **Step 1: Document current basket semantics and repaired tools.**
- [ ] **Step 2: Document GHCR deployment.**
- [ ] **Step 3: Open PR and inspect full diff.**
- [ ] **Step 4: Verify GitHub Actions checks pass.**
- [ ] **Step 5: Merge only after checks pass.**
- [ ] **Step 6: Confirm GHCR publish workflow completes on `main`.**