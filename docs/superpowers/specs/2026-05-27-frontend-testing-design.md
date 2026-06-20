# Introduce Vitest + Playwright Frontend Testing

## Summary

Introduce Vitest (unit + component) and Playwright (E2E) to fill the frontend testing gap. Zero existing frontend tests today. Full coverage across all 8 views, API layer, router guards, and error paths. Both tools run in CI on every PR.

## Tool Selection

| Layer | Tool | Role |
|-------|------|------|
| Unit + Component | `vitest` + `@vue/test-utils` + `happy-dom` + `msw` | Pure logic, composables, Vue component rendering |
| E2E | `@playwright/test` + Page Object Model | Real browser, full user flows |

**Why this stack:**
- **happy-dom** over jsdom: faster, lighter, Vitest-recommended default
- **MSW** over axios-mock-adapter: intercepts at the network layer, closer to real behavior, doesn't couple to axios internals
- **Page Object Model**: locators and actions centralized per page, change once when UI structure shifts

## Coverage Scope

### Vitest — Unit Tests (no component mount)

| Target | What to test |
|--------|-------------|
| `api/index.ts` (6 modules, ~30 endpoints) | Each function: verify HTTP method, URL path, params/body shape via MSW |
| `api/request.ts` | Token injection interceptor (reads localStorage), 401 response → `removeItem('token')` + `window.location.href = '/login'` |
| `router/index.ts` | `beforeEach` guard: no token + requiresAuth → `/login`; has token + `/login` → `/` |
| `App.vue` computed | `activeMenu`, `pageTitle`, `isLoginPage` — path-to-output mappings |

### Vitest — Component Tests (mount + interact)

| Component | Key coverage |
|-----------|-------------|
| **Login.vue** | Form render, v-model binding, submit loading state, success → localStorage + router.push, failure → error alert, Enter key triggers login |
| **Dashboard.vue** | 4 stat cards rendering, `successRate` computed (including divide-by-zero), `progressColor` thresholds (>=95 / >=80 / <80), `failColor`, recent executions table, empty state |
| **TaskList.vue** | Table/topology view toggle, search + filter → API call, pagination, enable/disable switch, runTask loading state, delete popconfirm, drawer timeline, `layoutData` Kahn algorithm computed (given input tasks + deps → assert levels and coordinates) |
| **TaskEdit.vue** | New vs edit mode, form backfill, cron macro selection, dependency selector, save/create submission |
| **GroupList.vue** | Group list render, delete confirmation |
| **GroupEdit.vue** | New/edit mode, cron parsing + next-run preview, member drag-and-drop ordering, save |
| **ExecutionLogs.vue** | Virtual scroll table, multi-filter search, CSV/JSON export, Clear All confirmation |
| **Settings.vue** | Settings render, save submission |
| **App.vue** | Sidebar menu highlight, login page standalone layout, logout clears token |

### Vitest — Not In Scope

- Element Plus internal component behavior (library tested upstream)
- CSS visual details (visual regression excluded)
- happy-dom unsupported browser APIs (polyfilled in vitest.config)

### Playwright — Happy Paths

| Page | Cases |
|------|-------|
| **Login** | Valid credentials → redirect to Dashboard; empty fields → error shown |
| **Dashboard** | Stat cards show correct values; success rate ring rendered; recent executions table populated |
| **TaskList** | List loads; search filters; pagination works; New Task button navigates; enable/disable toggle; manual run trigger; log drawer opens; delete confirm |
| **TaskEdit** | Create new task full form → submit → redirect to list; edit existing task → form backfilled → modify and save |
| **GroupList** | List loads; New navigates; delete confirm |
| **GroupEdit** | Create group → select mode → add members → save; edit existing → add/remove members → save |
| **ExecutionLogs** | List loads; status filter; time range filter; task name search; CSV export; JSON export; Clear All |
| **Settings** | Default values rendered; modify and save |

### Playwright — Error Branches & Edge Cases

| Scenario | Assertion |
|----------|-----------|
| 401 token expired | Request with invalid token → redirected to `/login` |
| Network failure | API unreachable → page doesn't crash, user sees feedback |
| Empty data | No tasks / no logs / no groups → empty state rendered, no white screen |
| Double-click guard | Rapid double-click Run → only one request sent |
| 404 route | Visit non-existent path → no white screen |
| Auth guard bypass | Visit `/tasks` without login → redirected to `/login` |

### Playwright — Not In Scope

- Cross-browser matrix (Chrome only; add Firefox/Safari when needed)
- Mobile viewport testing
- Visual regression / screenshot diffing

## CI Strategy

```
build job (existing)
  npm ci
  npm run build
  npx vitest run          ← NEW: unit + component (seconds)
  go test ./internal/...

e2e job (NEW, needs: build)
  Download binary artifact
  Start cronix server
  npx playwright test     ← E2E (minutes)
  Upload screenshots/traces on failure
```

- Vitest runs in `build` job (fast, no extra job overhead)
- Playwright in separate `e2e` job (doesn't slow down build feedback)
- Artifacts uploaded only on failure to save storage

## New npm Scripts

```json
{
  "test": "vitest run",
  "test:watch": "vitest",
  "test:e2e": "playwright test",
  "test:e2e:ui": "playwright test --ui"
}
```

## Directory Layout

```
web/
├── src/                         # unchanged
├── tests/
│   ├── unit/
│   │   ├── api.spec.ts
│   │   ├── request.spec.ts
│   │   └── router.spec.ts
│   ├── components/
│   │   ├── Login.spec.ts
│   │   ├── Dashboard.spec.ts
│   │   ├── TaskList.spec.ts
│   │   ├── TaskEdit.spec.ts
│   │   ├── GroupList.spec.ts
│   │   ├── GroupEdit.spec.ts
│   │   ├── ExecutionLogs.spec.ts
│   │   ├── Settings.spec.ts
│   │   └── App.spec.ts
│   ├── e2e/
│   │   ├── pages/
│   │   │   ├── LoginPage.ts
│   │   │   ├── DashboardPage.ts
│   │   │   ├── TaskListPage.ts
│   │   │   ├── TaskEditPage.ts
│   │   │   ├── GroupListPage.ts
│   │   │   ├── GroupEditPage.ts
│   │   │   ├── ExecutionLogsPage.ts
│   │   │   └── SettingsPage.ts
│   │   ├── specs/
│   │   │   ├── login.spec.ts
│   │   │   ├── dashboard.spec.ts
│   │   │   ├── tasks.spec.ts
│   │   │   ├── groups.spec.ts
│   │   │   ├── logs.spec.ts
│   │   │   ├── settings.spec.ts
│   │   │   └── auth-guard.spec.ts
│   │   └── fixtures/
│   │       ├── auth.ts
│   │       └── data.ts
│   └── mocks/
│       └── handlers.ts
├── vitest.config.ts
├── playwright.config.ts
└── package.json
```

## New Dependencies

```json
// devDependencies additions
"vitest": "^3.0",
"@vue/test-utils": "^2.4",
"happy-dom": "^16.0",
"msw": "^2.7",
"@playwright/test": "^1.50"
```

## Known Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| Deep Element Plus DOM (el-table-v2, el-drawer, el-popconfirm) → brittle locators | Prefer `data-testid`, fallback to `getByRole`/`getByText`; avoid CSS class chains |
| DAG SVG topology assertions are hard | Unit-test `layoutData` computed in Vitest (input tasks + deps → assert levels/coordinates); Playwright only verifies SVG node count |
| MSW + axios interceptors interaction | MSW runs in Node `server` mode for Vitest, bypasses Service Worker; axios interceptors fully controllable |
| happy-dom lacks ResizeObserver etc. → mount errors | Mock missing APIs in vitest.config setup; Element Plus community has established patterns |
| Playwright browser binary on first CI run | Explicit `playwright install chromium --with-deps` in CI step |

## Implementation Phases

1. **Infrastructure** — install deps, vitest.config.ts, playwright.config.ts, MSW handlers, data-testid attributes
2. **Vitest Unit** — api.spec.ts → request.spec.ts → router.spec.ts
3. **Vitest Component** — Login → Dashboard → TaskList → TaskEdit → GroupList → GroupEdit → ExecutionLogs → Settings → App
4. **Playwright E2E** — Page Objects → fixtures → specs (happy path first, then error branches)
5. **CI Integration** — modify build.yml, verify all green

Each phase: all tests must pass before moving to the next.
