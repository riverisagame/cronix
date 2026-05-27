# Implementation Plan: Vitest + Playwright Frontend Testing

## Summary

5 phases to introduce Vitest (unit + component) and Playwright (E2E) into the cronix frontend with CI integration. Each phase is independently verifiable.

---

## Phase 1: Infrastructure

### 1.1 Install Dependencies

```bash
cd web
npm install -D vitest @vue/test-utils happy-dom msw @playwright/test
npx playwright install chromium --with-deps
```

### 1.2 Create `web/vitest.config.ts`

- Extend existing Vite config (resolve aliases, proxy not needed)
- Set `environment: 'happy-dom'`
- Setup file: mock `ResizeObserver`, `IntersectionObserver`, `matchMedia`
- Include `tests/**/*.spec.ts`

### 1.3 Create `web/playwright.config.ts`

- Target `http://localhost:8080` (served by Go binary serving both API + static)
- Single browser: `chromium`
- Output dir: `tests/e2e/results`
- Screenshots + trace on failure only
- Global setup: login helper to get token

### 1.4 Create `web/tests/mocks/handlers.ts`

- MSW handlers for all ~30 API endpoints
- Each handler returns a realistic shaped response matching the backend contract
- Default: success responses with sample data
- Individual tests override handlers for error cases via `server.use()`

### 1.5 Add `data-testid` Attributes

Add `data-testid` to key interactive elements across all 8 components. Focus on:
- Buttons (submit, delete, run, export, toggle)
- Inputs (search, form fields)
- Navigation items
- Status indicators
- Table rows and cells

### Phase 1 Verification

```bash
cd web
npx vitest run              # runs 0 tests (config loads cleanly)
npx playwright test --dry   # config valid
npm run build               # still builds
```

---

## Phase 2: Vitest Unit Tests

### 2.1 `tests/unit/api.spec.ts`

- For each of the 6 API modules (authAPI, taskAPI, logAPI, dashboardAPI, settingsAPI, groupAPI)
- MSW intercepts requests
- Call each function → assert the outgoing request (method, URL, params, body, headers)
- ~30 test cases

### 2.2 `tests/unit/request.spec.ts`

- Test: request includes `Authorization: Bearer <token>` when token in localStorage
- Test: request does NOT include Authorization when no token
- Test: 401 response → `localStorage.removeItem('token')` called
- Test: 401 response → `window.location.href` changed to `/login`
- Test: non-401 error → `Promise.reject` propagated

### 2.3 `tests/unit/router.spec.ts`

- Test: `/tasks` without token → redirect to `/login`
- Test: `/groups` without token → redirect to `/login`
- Test: `/settings` without token → redirect to `/login`
- Test: `/` without token → redirect to `/login`
- Test: `/login` with token → redirect to `/`
- Test: Non-protected `/login` without token → proceed normally
- Test: Protected route with token → proceed normally

### Phase 2 Verification

```bash
npx vitest run  # all unit tests green (~40 tests)
```

---

## Phase 3: Vitest Component Tests

For each component, mount with `@vue/test-utils`, mock dependencies (vue-router, Element Plus icons, MSW handlers for API), and test the documented behaviors from the spec.

### 3.1 `tests/components/Login.spec.ts`
### 3.2 `tests/components/Dashboard.spec.ts`
### 3.3 `tests/components/TaskList.spec.ts`
### 3.4 `tests/components/TaskEdit.spec.ts`
### 3.5 `tests/components/GroupList.spec.ts`
### 3.6 `tests/components/GroupEdit.spec.ts`
### 3.7 `tests/components/ExecutionLogs.spec.ts`
### 3.8 `tests/components/Settings.spec.ts`
### 3.9 `tests/components/App.spec.ts`

### Phase 3 Verification

```bash
npx vitest run  # all unit + component tests green (~80-100 tests)
```

---

## Phase 4: Playwright E2E Tests

### 4.1 Page Objects

Create all 8 page classes under `tests/e2e/pages/`. Each class:
- Constructor receives `page: Page`
- Properties: URL path, key locators (via `data-testid` + semantic selectors)
- Methods: `navigate()`, action methods (fill, click, select), assertion methods (waitFor, expect)

### 4.2 Fixtures

- `tests/e2e/fixtures/auth.ts`: `login(page)` helper — navigates to `/login`, fills credentials, clicks submit, waits for redirect
- `tests/e2e/fixtures/data.ts`: seed data helpers or constants

### 4.3 Happy Path Specs

Write `tests/e2e/specs/*.spec.ts` for all 6 page groups per the spec's happy path list.

### 4.4 Error Branch Specs

- `tests/e2e/specs/auth-guard.spec.ts`: 401 handling, protected route redirects
- Additional error cases inside each page spec (empty submit, network failure simulation)

### Phase 4 Verification

```bash
# Start the dev server first
npx playwright test  # all E2E green (~30-40 test cases)
```

---

## Phase 5: CI Integration

### 5.1 Modify `.github/workflows/build.yml`

**In `build` job**, after `npm run build`:
```yaml
- name: "Run frontend unit tests"
  working-directory: web
  run: npx vitest run
```

**New `e2e` job**, after `verify`:
```yaml
e2e:
  needs: build
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    
    - uses: actions/setup-node@v4
      with:
        node-version: "20"
        cache: npm
        cache-dependency-path: web/package-lock.json
    
    - name: "Install frontend deps"
      working-directory: web
      run: npm ci
    
    - name: "Install Playwright browsers"
      working-directory: web
      run: npx playwright install chromium --with-deps
    
    - name: "Download binary"
      uses: actions/download-artifact@v4
      with:
        name: cronix-linux-amd64
    
    - name: "Start server"
      run: |
        chmod +x cronix-linux-amd64
        cp config.yaml /tmp/e2e-config.yaml
        # configure test password, use temp DB
        ./cronix-linux-amd64 serve -c /tmp/e2e-config.yaml &
        # wait for healthy
        for i in $(seq 1 15); do
          sleep 1
          if curl -sf http://localhost:8080/api/health > /dev/null 2>&1; then
            break
          fi
        done
    
    - name: "Run Playwright tests"
      working-directory: web
      run: npx playwright test
    
    - name: "Upload test results on failure"
      if: failure()
      uses: actions/upload-artifact@v4
      with:
        name: playwright-results
        path: web/tests/e2e/results
        retention-days: 3
```

### Phase 5 Verification

Push a PR → CI runs all 3 jobs (build, verify, e2e) → all green. Merge.

---

## File Change Summary

| File | Action |
|------|--------|
| `web/vitest.config.ts` | Create |
| `web/playwright.config.ts` | Create |
| `web/tests/mocks/handlers.ts` | Create |
| `web/tests/unit/api.spec.ts` | Create |
| `web/tests/unit/request.spec.ts` | Create |
| `web/tests/unit/router.spec.ts` | Create |
| `web/tests/components/Login.spec.ts` | Create |
| `web/tests/components/Dashboard.spec.ts` | Create |
| `web/tests/components/TaskList.spec.ts` | Create |
| `web/tests/components/TaskEdit.spec.ts` | Create |
| `web/tests/components/GroupList.spec.ts` | Create |
| `web/tests/components/GroupEdit.spec.ts` | Create |
| `web/tests/components/ExecutionLogs.spec.ts` | Create |
| `web/tests/components/Settings.spec.ts` | Create |
| `web/tests/components/App.spec.ts` | Create |
| `web/tests/e2e/pages/LoginPage.ts` | Create |
| `web/tests/e2e/pages/DashboardPage.ts` | Create |
| `web/tests/e2e/pages/TaskListPage.ts` | Create |
| `web/tests/e2e/pages/TaskEditPage.ts` | Create |
| `web/tests/e2e/pages/GroupListPage.ts` | Create |
| `web/tests/e2e/pages/GroupEditPage.ts` | Create |
| `web/tests/e2e/pages/ExecutionLogsPage.ts` | Create |
| `web/tests/e2e/pages/SettingsPage.ts` | Create |
| `web/tests/e2e/specs/login.spec.ts` | Create |
| `web/tests/e2e/specs/dashboard.spec.ts` | Create |
| `web/tests/e2e/specs/tasks.spec.ts` | Create |
| `web/tests/e2e/specs/groups.spec.ts` | Create |
| `web/tests/e2e/specs/logs.spec.ts` | Create |
| `web/tests/e2e/specs/settings.spec.ts` | Create |
| `web/tests/e2e/specs/auth-guard.spec.ts` | Create |
| `web/tests/e2e/fixtures/auth.ts` | Create |
| `web/tests/e2e/fixtures/data.ts` | Create |
| `web/src/views/*.vue` (8 files) | Edit: add data-testid attributes |
| `web/package.json` | Edit: add test scripts + dependencies |
| `.github/workflows/build.yml` | Edit: add Vitest to build job, new e2e job |
