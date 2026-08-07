# CameraIO Bright Chrome 72 UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign CameraIO's web console as a modern, bright, flat operations UI while retaining every current camera, preview, snapshot, recording, and scheduling workflow in Google Chrome 72.

**Architecture:** Keep Vue 3, Vue Router, Tailwind, and the existing API contracts. Introduce a small presentation layer in `assets/main.css` for shared tokens and reusable UI primitives, then apply those primitives to the layout and existing pages without moving API calls or stateful camera/recording logic. Configure Vite and Autoprefixer for Chrome 72; add explicit CSS fallbacks for Flexbox gaps and `aspect-ratio`, which Chrome 72 does not support.

**Tech Stack:** Vue 3.5, Vue Router 4, Vite 5, Tailwind CSS 3, PostCSS/Autoprefixer, Node built-in test runner, `@vitejs/plugin-legacy` 5, `core-js` 3.

## Global Constraints

- Support Google Chrome **72** on Windows 7/10 as a release requirement.
- Build target is exactly `chrome72`; do not ship syntax requiring optional chaining, nullish coalescing, class fields, or CSS `aspect-ratio` without a fallback.
- Do not use Flexbox `gap` as the only source of spacing; Chrome 72 lacks Flexbox-gap support.
- Preserve all existing routes, API paths, camera actions, snapshot behaviour, WebSocket behaviour, recording actions, and Chinese copy unless a copy change is explicitly listed below.
- Keep the overall theme bright: light canvas, white surfaces, blue primary action, compact borders, restrained shadows, and no dark navigation shell.
- Keep all iconography SVG through the existing `AppIcon.vue`; do not add icon fonts, emoji controls, or browser-dependent Unicode glyphs.
- Maintain keyboard focus visibility, 4.5:1 text contrast for normal text, and a 40px minimum pointer target for primary actions.
- Do not add a browser requirement newer than Chrome 72 or a runtime dependency that needs CGO.

---

## File Structure

- Modify: `frontend/package.json` — add the compatibility build/test commands and the browser compatibility dependencies.
- Modify: `frontend/vite.config.js` — emit Chrome 72-compatible JavaScript and CSS bundles.
- Modify: `frontend/postcss.config.js` — consume the shared browser target through Autoprefixer.
- Create: `frontend/.browserslistrc` — make `Chrome >= 72` the single source of browser policy for Autoprefixer and compatibility tooling.
- Create: `frontend/src/compat.js` — load the narrowly scoped runtime polyfills required by the Chrome 72 target before Vue starts.
- Create: `frontend/scripts/assert-chrome72-build.mjs` — reject unsupported modern syntax and require the CSS compatibility fallbacks in the built output.
- Create: `frontend/src/compat.test.mjs` — execute the compatibility assertion against a temporary production build in CI.
- Modify: `frontend/src/main.js` — load compatibility support before mounting the Vue app.
- Modify: `frontend/src/assets/main.css` — define bright design tokens, base typography, reusable primitive classes, focus styles, and Chrome 72 CSS fallbacks.
- Modify: `frontend/src/components/Layout.vue` — provide the bright responsive application shell, active navigation, mobile navigation toggle, and shared page frame.
- Modify: `frontend/src/views/Login.vue` — apply the new bright authentication layout without changing login state/error handling.
- Modify: `frontend/src/views/Cameras.vue` — restyle camera cards, actions, forms, dialogs, scan results, and status presentation while preserving the existing methods and `v-model` data.
- Modify: `frontend/src/views/Live.vue` — restyle the live monitoring control bar, stream tiles, recording dialog, and snapshot modal without changing stream lifecycles.
- Modify: `frontend/src/views/Recordings.vue` — restyle tabs, filters, table, schedules, and dialogs; add small-screen table overflow rather than hiding operational columns.
- Modify: `frontend/src/components/FfmpegBanner.vue` — align alert states with the shared bright warning/error primitive.
- Modify: `frontend/src/api.test.mjs` — retain the current snapshot transport tests and add any required assertions if UI-facing API helpers are changed.
- Create: `frontend/src/ui-smoke.test.mjs` — static DOM/CSS contract tests for the light shell, focusable navigation, and compatibility classes.
- Modify: `.github/workflows/test.yml` — run frontend unit tests, production build, and the Chrome 72 artifact assertion on Linux CI.

## Task 1: Establish and test the Chrome 72 build contract

**Files:**
- Create: `frontend/.browserslistrc`
- Create: `frontend/src/compat.js`
- Create: `frontend/scripts/assert-chrome72-build.mjs`
- Create: `frontend/src/compat.test.mjs`
- Modify: `frontend/package.json`
- Modify: `frontend/vite.config.js`
- Modify: `frontend/postcss.config.js`
- Modify: `frontend/src/main.js`

**Interfaces:**
- Consumes: Vite's `build.target` and PostCSS Autoprefixer browser configuration.
- Produces: `npm run test:chrome72`, which exits non-zero if a production bundle contains unsupported syntax or omits the CSS fallback marker.

- [ ] **Step 1: Write the failing compatibility test**

Create `frontend/src/compat.test.mjs`:

```js
import test from 'node:test'
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'

test('production bundle satisfies the Chrome 72 compatibility contract', () => {
  execFileSync('npm', ['run', 'build'], { stdio: 'inherit' })
  const result = execFileSync('node', ['scripts/assert-chrome72-build.mjs'], { encoding: 'utf8' })
  assert.match(result, /Chrome 72 compatibility check passed/)
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `node --test src/compat.test.mjs`

Expected: FAIL because `scripts/assert-chrome72-build.mjs` and the Chrome 72 build configuration do not exist.

- [ ] **Step 3: Add the browser policy and Vite compatibility configuration**

Create `frontend/.browserslistrc` with exactly:

```text
Chrome >= 72
```

Update `frontend/vite.config.js` to use the Vue plugin plus `@vitejs/plugin-legacy`, with the following effective configuration:

```js
build: {
  target: 'chrome72',
  cssTarget: 'chrome72',
},
plugins: [
  vue(),
  legacy({
    targets: ['Chrome >= 72'],
    modernPolyfills: ['es.object.from-entries', 'es.array.flat'],
    renderLegacyChunks: false,
  }),
],
```

Add `@vitejs/plugin-legacy` and `core-js` as production-build dependencies in `package.json`. Change the test script to `"test": "node --test"` so Node discovers every `*.test.mjs` file on Linux and Windows. Update `postcss.config.js` so Autoprefixer reads the `.browserslistrc` policy instead of an inline modern default.

- [ ] **Step 4: Add only the required runtime polyfills**

Create `frontend/src/compat.js`:

```js
import 'core-js/modules/es.array.flat.js'
import 'core-js/modules/es.array.flat-map.js'
import 'core-js/modules/es.object.from-entries.js'
```

Add `import './compat'` before the existing CSS import in `frontend/src/main.js`. Do not import `core-js/stable`; the bundle must contain only the listed compatibility polyfills.

- [ ] **Step 5: Implement the artifact assertion**

Create `frontend/scripts/assert-chrome72-build.mjs` that:

1. Reads every `dist/assets/*.js` file.
2. Fails if it finds `?.`, `??`, `#private`, or `static {`.
3. Reads every `dist/assets/*.css` file.
4. Fails unless the CSS contains `--chrome72-flex-gap-fallback` and matches `/@supports not\s*\(aspect-ratio\s*:\s*1\s*\/\s*1\)/`.
5. Prints exactly `Chrome 72 compatibility check passed` on success.

Add this script to `package.json`:

```json
"test:chrome72": "npm run build && node scripts/assert-chrome72-build.mjs"
```

- [ ] **Step 6: Run the compatibility test to verify it passes**

Run: `node --test src/compat.test.mjs && npm run test:chrome72`

Expected: PASS and the artifact assertion prints `Chrome 72 compatibility check passed`.

- [ ] **Step 7: Commit the compatibility contract**

```bash
git add frontend/.browserslistrc frontend/package.json frontend/package-lock.json frontend/postcss.config.js frontend/vite.config.js frontend/src/main.js frontend/src/compat.js frontend/src/compat.test.mjs frontend/scripts/assert-chrome72-build.mjs
git commit -m "build: target Chrome 72 frontend"
```

## Task 2: Create the bright flat design system and legacy CSS fallbacks

**Files:**
- Modify: `frontend/src/assets/main.css`
- Create: `frontend/src/ui-smoke.test.mjs`

**Interfaces:**
- Consumes: the `--chrome72-flex-gap-fallback` marker required by Task 1.
- Produces: reusable classes `app-shell`, `page-frame`, `ui-card`, `ui-button-primary`, `ui-button-secondary`, `ui-icon-button`, `ui-input`, `ui-status`, and `ui-modal` used by all views.

- [ ] **Step 1: Write the failing design-system contract test**

Create `frontend/src/ui-smoke.test.mjs`:

```js
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const css = readFileSync(new URL('./assets/main.css', import.meta.url), 'utf8')

test('bright design system exposes shared primitives and Chrome 72 fallbacks', () => {
  for (const selector of [
    '.app-shell', '.ui-card', '.ui-button-primary', '.ui-input', '.ui-status',
    '--chrome72-flex-gap-fallback', '@supports not (aspect-ratio: 1 / 1)',
  ]) {
    assert.ok(css.includes(selector), `missing ${selector}`)
  }
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `node --test src/ui-smoke.test.mjs`

Expected: FAIL because the shared primitives and fallback marker do not exist.

- [ ] **Step 3: Implement the token layer and flat primitives**

In `main.css`, define these semantic tokens at `:root`:

```css
--canvas: #f6f8fc;
--surface: #ffffff;
--surface-muted: #f1f5f9;
--line: #dbe3ee;
--text: #172033;
--text-muted: #64748b;
--primary: #2563eb;
--primary-hover: #1d4ed8;
--success: #16a34a;
--danger: #dc2626;
--radius: 10px;
--shadow-card: 0 8px 24px rgba(15, 23, 42, 0.06);
```

Use them to implement the Task 2 primitive classes. Cards must use one thin border and `--shadow-card`; no gradient background, glass blur, or heavy `shadow-xl` may be used outside a modal. Add `:focus-visible` outlines for links, buttons, inputs, selects, and textareas.

- [ ] **Step 4: Implement Chrome 72 CSS fallbacks**

Use `--chrome72-flex-gap-fallback: 1` in `:root` and add fallback selectors for all spacing utilities the UI will use:

```css
@supports not (gap: 1rem) {
  .compat-flex-gap-1 > * + * { margin-left: 0.25rem; }
  .compat-flex-gap-2 > * + * { margin-left: 0.5rem; }
  .compat-flex-gap-3 > * + * { margin-left: 0.75rem; }
  .compat-flex-gap-4 > * + * { margin-left: 1rem; }
  .compat-flex-column > * + * { margin-top: 0.75rem; }
}
@supports not (aspect-ratio: 1 / 1) {
  .compat-aspect-video { height: 0; padding-top: 56.25%; position: relative; }
  .compat-aspect-video > img,
  .compat-aspect-video > video { position: absolute; inset: 0; width: 100%; height: 100%; }
}
```

Use `compat-flex-gap-*`, `compat-flex-column`, and `compat-aspect-video` in subsequent view tasks instead of relying on Tailwind `gap-*` or `aspect-video` alone.

- [ ] **Step 5: Run the design-system test to verify it passes**

Run: `node --test src/ui-smoke.test.mjs && npm run test:chrome72`

Expected: PASS; the production CSS contains both fallback blocks.

- [ ] **Step 6: Commit the design system**

```bash
git add frontend/src/assets/main.css frontend/src/ui-smoke.test.mjs
git commit -m "feat: add bright compatible UI primitives"
```

## Task 3: Redesign the shared shell and authentication flow

**Files:**
- Modify: `frontend/src/components/Layout.vue`
- Modify: `frontend/src/components/FfmpegBanner.vue`
- Modify: `frontend/src/views/Login.vue`
- Modify: `frontend/src/ui-smoke.test.mjs`

**Interfaces:**
- Consumes: Task 2 primitive classes and browser-compatible spacing helpers.
- Produces: a shared `page-frame` layout with a responsive light sidebar on desktop and a compact mobile navigation control below 900px.

- [ ] **Step 1: Extend the failing UI contract test**

Append this test to `ui-smoke.test.mjs`:

```js
test('application shell uses a light navigation surface and an accessible mobile toggle', () => {
  const layout = readFileSync(new URL('./components/Layout.vue', import.meta.url), 'utf8')
  assert.match(layout, /app-shell/)
  assert.match(layout, /aria-label="切换导航"/)
  assert.doesNotMatch(layout, /bg-slate-900/)
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `node --test src/ui-smoke.test.mjs`

Expected: FAIL because the current shell uses a dark `bg-slate-900` sidebar and has no mobile navigation button.

- [ ] **Step 3: Implement the responsive bright shell**

In `Layout.vue`:

1. Keep `navItems`, `currentUser`, and `handleLogout` unchanged.
2. Add `const mobileNavOpen = ref(false)` and a top-bar button with `aria-label="切换导航"`, `:aria-expanded="mobileNavOpen"`, and `@click="mobileNavOpen = !mobileNavOpen"`.
3. Replace the dark `<aside>` with a white bordered navigation surface; use a blue active indicator and `AppIcon` exactly as today.
4. Close the mobile navigation after clicking any `router-link`.
5. Keep the desktop sidebar visible at `min-width: 900px`; below that width, show the compact top bar and slide the navigation over the page without blocking keyboard focus.
6. Wrap routed content in `page-frame` so all pages inherit consistent padding and maximum width.

In `FfmpegBanner.vue`, replace one-off warning styles with the shared warning/status classes while retaining its polling and action logic. In `Login.vue`, replace the dark gradient with `--canvas`, a compact white `ui-card`, blue primary button, and a restrained logo treatment.

- [ ] **Step 4: Run shell and existing API tests to verify they pass**

Run: `node --test src/ui-smoke.test.mjs src/api.test.mjs && npm run build`

Expected: PASS; existing snapshot request assertions remain unchanged.

- [ ] **Step 5: Manually verify responsive shell behavior**

Run: `npm run dev`

Verify at 1440px and 768px wide that navigation reaches Cameras, Live, and Recordings; the logout action remains available; focus rings are visible; and no text is placed on a dark full-page background.

- [ ] **Step 6: Commit the shell redesign**

```bash
git add frontend/src/components/Layout.vue frontend/src/components/FfmpegBanner.vue frontend/src/views/Login.vue frontend/src/ui-smoke.test.mjs
git commit -m "feat: redesign bright application shell"
```

## Task 4: Restyle camera management without changing operations

**Files:**
- Modify: `frontend/src/views/Cameras.vue`
- Modify: `frontend/src/ui-smoke.test.mjs`
- Test: `frontend/src/api.test.mjs`

**Interfaces:**
- Consumes: `ui-card`, button, status, form, modal, and compatibility spacing classes from Task 2.
- Produces: camera cards with an obvious primary action, readable protocol/status metadata, and browser-compatible add/edit/scan dialogs.

- [ ] **Step 1: Add a failing camera-view structure test**

Append to `ui-smoke.test.mjs`:

```js
test('camera management keeps the snapshot action and uses compatible video-free layout primitives', () => {
  const cameras = readFileSync(new URL('./views/Cameras.vue', import.meta.url), 'utf8')
  assert.match(cameras, /captureSnapshot/)
  assert.match(cameras, /ui-card/)
  assert.match(cameras, /compat-flex-gap-/)
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `node --test src/ui-smoke.test.mjs`

Expected: FAIL because `Cameras.vue` does not yet use the shared primitive or compatibility spacing classes.

- [ ] **Step 3: Apply the camera-management visual hierarchy**

1. Keep every existing `ref`, computed value, API import, handler name, dialog `v-model`, and endpoint unchanged.
2. Change the page heading into a compact `page-header`: title, online/total count, short supporting text, then secondary “扫描局域网” and primary “添加摄像头” actions.
3. Render each camera as a `ui-card` with: name/IP in the header, a coloured `ui-status` pill, protocol/device tags in a muted metadata row, and errors in a compact inline alert.
4. Make “预览” and “抓拍” visually distinct primary/secondary actions while retaining all existing guards, busy states, and capture modal behaviour.
5. Convert icon-only actions to `ui-icon-button`, retaining their `title` and adding `aria-label` values matching the Chinese action labels.
6. Restyle add/edit/network/scan dialogs with `ui-modal`, persistent close controls, labelled inputs, and 40px action buttons; preserve the current username/password-before-discovery form ordering.
7. Use `compat-flex-gap-*` in every new flex action row and avoid adding `aspect-ratio`.

- [ ] **Step 4: Run camera and API tests to verify they pass**

Run: `node --test src/ui-smoke.test.mjs src/api.test.mjs && npm run test:chrome72`

Expected: PASS; snapshot still requests `/api/v1/cameras/:id/snapshot` as a blob.

- [ ] **Step 5: Manually verify camera operations**

Run: `npm run dev`

Verify adding, editing, scanning, testing, syncing, previewing, taking a snapshot, and deleting a camera still invoke their prior actions; check the add/edit dialog at 1024px and 768px.

- [ ] **Step 6: Commit the camera page redesign**

```bash
git add frontend/src/views/Cameras.vue frontend/src/ui-smoke.test.mjs frontend/src/api.test.mjs
git commit -m "feat: modernize camera management UI"
```

## Task 5: Restyle live monitoring with Chrome 72 media layout fallback

**Files:**
- Modify: `frontend/src/views/Live.vue`
- Modify: `frontend/src/ui-smoke.test.mjs`

**Interfaces:**
- Consumes: `compat-aspect-video`, compatible flex spacing, shared modal and button classes.
- Produces: a bright monitoring workspace where only the media canvas is dark, with existing stream/record/snapshot lifecycles unchanged.

- [ ] **Step 1: Add a failing live-view compatibility test**

Append to `ui-smoke.test.mjs`:

```js
test('live view keeps media controls and applies the Chrome 72 aspect fallback', () => {
  const live = readFileSync(new URL('./views/Live.vue', import.meta.url), 'utf8')
  assert.match(live, /startStream/)
  assert.match(live, /stopStream/)
  assert.match(live, /compat-aspect-video/)
  assert.match(live, /captureSnapshot/)
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `node --test src/ui-smoke.test.mjs`

Expected: FAIL because current video tiles only use Tailwind's `aspect-video` utility.

- [ ] **Step 3: Implement the monitoring workspace**

1. Preserve all stream URL caching, stream start/stop calls, event bus handling, recording calls, and snapshot cleanup methods.
2. Create a white `ui-card` toolbar for grid size, camera totals, current time, refresh, and global actions; make controls wrap using compatibility spacing at narrow widths.
3. Keep each video area dark for contrast, but place it inside a light bordered tile with camera name, connection state, and action strip.
4. Add `compat-aspect-video` to every MJPEG/video container in addition to any Tailwind aspect utility.
5. Restyle recording controls and the snapshot modal with `ui-modal`, visible loading/error state, semantic labels, and keyboard-accessible close buttons.
6. Keep the grid usable at 1x1, 2x2, 3x3, and 4x4 without relying on Flexbox `gap`.

- [ ] **Step 4: Run the live-view contract and production compatibility checks**

Run: `node --test src/ui-smoke.test.mjs src/api.test.mjs && npm run test:chrome72`

Expected: PASS.

- [ ] **Step 5: Manually verify the stream lifecycle**

With one reachable test camera, verify start preview, stop preview, grid-size switch, native snapshot, start recording, stop recording, and closing the snapshot modal. Confirm the browser network panel has one stable MJPEG request per visible stream.

- [ ] **Step 6: Commit the live page redesign**

```bash
git add frontend/src/views/Live.vue frontend/src/ui-smoke.test.mjs
git commit -m "feat: modernize live monitoring workspace"
```

## Task 6: Restyle recordings and schedules, then wire the complete CI gate

**Files:**
- Modify: `frontend/src/views/Recordings.vue`
- Modify: `frontend/src/ui-smoke.test.mjs`
- Modify: `.github/workflows/test.yml`
- Modify: `frontend/package.json`

**Interfaces:**
- Consumes: shared primitives and compatibility scripts from Tasks 1-2.
- Produces: an operations-friendly recording/schedule page and a CI frontend gate that verifies unit tests, production output, and Chrome 72 artifact policy.

- [ ] **Step 1: Add a failing recordings-view contract test**

Append to `ui-smoke.test.mjs`:

```js
test('recordings view preserves recording and schedule actions in a responsive surface', () => {
  const recordings = readFileSync(new URL('./views/Recordings.vue', import.meta.url), 'utf8')
  assert.match(recordings, /stopRecording/)
  assert.match(recordings, /saveSchedule/)
  assert.match(recordings, /ui-card/)
  assert.match(recordings, /overflow-x-auto/)
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `node --test src/ui-smoke.test.mjs`

Expected: FAIL because the recordings surface has not yet adopted the shared card/table primitives.

- [ ] **Step 3: Implement the recordings and scheduling visual update**

1. Retain all filtering, pagination, preview, download, stop, delete, schedule CRUD, and toggle handlers.
2. Convert the tab switcher and filters into a flat white `ui-card` toolbar with a blue active state and clear count summary.
3. Place the recordings table in `overflow-x-auto`; do not drop columns on small screens. Use a sticky-looking visual header only when it works without `position: sticky` assumptions.
4. Use compact status pills for recording/completed/failed and primary/secondary/icon button primitives for preview, download, stop, and delete.
5. Restyle schedules as light cards with consistent day/time metadata and clear enabled/disabled state.
6. Apply `ui-modal`, `compat-flex-gap-*`, and labelled focusable controls to recording preview and schedule dialogs.

- [ ] **Step 4: Update the CI frontend gate**

In `.github/workflows/test.yml`, change the existing frontend job name to `Frontend Tests and Build`, and run these commands in order from `frontend/`:

```yaml
- run: npm ci
- run: npm test
- run: npm run test:chrome72
```

Do not add a separate duplicate `npm run build` step because `test:chrome72` already performs the production build.

- [ ] **Step 5: Run the complete frontend verification suite**

Run: `npm test && npm run test:chrome72`

Expected: all API, UI contract, and compatibility checks pass; Vite emits the Chrome 72-targeted production bundle.

- [ ] **Step 6: Manually verify Chrome 72**

Open the built application in an actual Chrome 72 browser or Windows 7 virtual machine. Verify login, navigation, camera list, live 1x1/2x2 preview layout, snapshot modal, recordings table horizontal scrolling, schedule dialog, focus rings, and light theme at 1280px and 1024px. Record the browser version and results in the pull request description.

- [ ] **Step 7: Commit the recordings page and CI gate**

```bash
git add frontend/src/views/Recordings.vue frontend/src/ui-smoke.test.mjs frontend/package.json .github/workflows/test.yml
git commit -m "feat: refresh recordings UI for Chrome 72"
```

## Final Verification

- [ ] Run `git diff --check`.
- [ ] Run `CGO_ENABLED=0 go test -count=1 ./...` from the repository root.
- [ ] Run `npm test && npm run test:chrome72` from `frontend/`.
- [ ] Run `CGO_ENABLED=0 go build -o /tmp/cameraio ./cmd/server` after the frontend build to verify `//go:embed` packaging.
- [ ] Run `CGO_ENABLED=0 go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/test.yml`.
- [ ] Confirm Chrome 72 manual acceptance results are recorded before claiming browser support complete.

## Plan Self-Review

- Chrome 72 JavaScript, CSS Flexbox-gap, and `aspect-ratio` constraints are covered in Tasks 1-2 and verified again in Tasks 5-6.
- The UI is bright, flat, responsive, and modern through Tasks 2-6; dark styling is limited to video canvases where it improves media contrast.
- Camera, stream, snapshot, recording, schedule, and API behaviour are explicitly preserved in Tasks 3-6.
- The plan contains an automated production artifact policy plus a required real-browser Chrome 72 acceptance test; automated checks alone cannot prove rendering fidelity on that historical browser.
