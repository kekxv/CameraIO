# Windows 7 Icon Compatibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace frontend Emoji and Dingbat icons that appear as square glyphs on Windows 7 with browser-rendered SVG icons or plain Chinese text.

**Architecture:** Add one allow-listed Vue SVG icon component whose path data is rendered by the browser and reuse it across navigation, camera, live, recording, dialog, and warning UI. Remove Emoji from native alert strings because system dialogs cannot use the web SVG component.

**Tech Stack:** Vue 3, Vite, Tailwind CSS, inline SVG.

## Global Constraints

- Do not add an icon-font or third-party icon dependency.
- Do not use `v-html` for icon markup.
- Icons must render independently of Windows system Emoji fonts.
- Preserve existing click handlers, labels, colors, and layouts.

---

### Task 1: Create the reusable SVG icon component

**Files:**
- Create: `frontend/src/components/AppIcon.vue`
- Modify: `frontend/src/components/Layout.vue`
- Modify: `frontend/src/components/FfmpegBanner.vue`

**Interfaces:**
- Consumes: an allow-listed `name` prop and inherited Tailwind `class` attributes.
- Produces: `<AppIcon name="camera" />`-style outline icons rendered as a root `<svg>`.

- [ ] **Step 1: Implement `AppIcon.vue`**

Define path arrays for the names used by the application (`camera`, `monitor`, `film`, `warning`, `scan`, `clock`, `plug`, `globe`, `edit`, `trash`, `search`, `refresh`, `info`, `eye`, `download`, `stop`, `calendar`, `play`, `pause`, and `close`). Render allow-listed path `d` values with `fill="none"`, `stroke="currentColor"`, `stroke-width="1.8"`, and `aria-hidden="true"`.

- [ ] **Step 2: Replace shell and FFmpeg banner Emoji**

Import `AppIcon` into `Layout.vue` and `FfmpegBanner.vue`. Change navigation item values from Emoji to icon names, replace the `v-html` span with `<AppIcon>`, and replace the warning Emoji with the `warning` icon.

- [ ] **Step 3: Build the frontend**

Run: `cd frontend && npm run build`

Expected: Vite build exits 0.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/AppIcon.vue frontend/src/components/Layout.vue frontend/src/components/FfmpegBanner.vue
git commit -m "feat: add Windows-safe SVG icons"
```

### Task 2: Replace camera and live page Emoji

**Files:**
- Modify: `frontend/src/views/Cameras.vue`
- Modify: `frontend/src/views/Live.vue`

**Interfaces:**
- Consumes: `AppIcon` names from Task 1.
- Produces: camera and live pages without system-dependent icon glyphs.

- [ ] **Step 1: Replace template glyphs**

Import `AppIcon` in both views. Replace scan, empty-state, warning, time sync, connection test, network, edit, delete, search, refresh, device-brand indicator, information, camera, recording, and record-control glyphs with SVG components. For conditional button labels, split the icon and text into sibling elements controlled with `v-if` rather than embedding Emoji in strings.

- [ ] **Step 2: Replace native dialog Emoji**

Change alert messages from `✅ ...`, `❌ ...`, and `⚠️ ...` to plain prefixes `连接成功：`, `连接失败：`, and `注意：`, preserving diagnostic text.

- [ ] **Step 3: Build the frontend**

Run: `cd frontend && npm run build`

Expected: Vite build exits 0.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/views/Cameras.vue frontend/src/views/Live.vue
git commit -m "fix: replace camera page emoji icons"
```

### Task 3: Replace recording page Emoji and audit the bundle sources

**Files:**
- Modify: `frontend/src/views/Recordings.vue`

**Interfaces:**
- Consumes: `AppIcon` names from Task 1.
- Produces: recording list, scheduler, dialogs, and action buttons without system-dependent icon glyphs.

- [ ] **Step 1: Replace all recording view glyphs**

Import `AppIcon`. Replace tab, empty-state, preview, download, stop, delete, clock, calendar, format, pause/play, edit, and close glyphs. Add or preserve text/title labels for icon-only controls.

- [ ] **Step 2: Audit frontend source characters**

Run: `rg -n --pcre2 '[\x{1F000}-\x{1FAFF}\x{2600}-\x{27BF}]' frontend/src --glob '*.vue' --glob '*.js'`

Expected: no matches for visible Emoji/Dingbat icons.

- [ ] **Step 3: Build the frontend**

Run: `cd frontend && npm run build`

Expected: Vite build exits 0.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/views/Recordings.vue
git commit -m "fix: replace recording page emoji icons"
```

### Task 4: Final compatibility verification

**Files:**
- Verify: `frontend/src/components/AppIcon.vue`
- Verify: `frontend/src/components/Layout.vue`
- Verify: `frontend/src/components/FfmpegBanner.vue`
- Verify: `frontend/src/views/Cameras.vue`
- Verify: `frontend/src/views/Live.vue`
- Verify: `frontend/src/views/Recordings.vue`

**Interfaces:**
- Consumes: all icon replacements.
- Produces: a production frontend bundle suitable for Windows 7 browsers supported by the existing application.

- [ ] **Step 1: Run a clean production build**

Run: `cd frontend && npm run build`

Expected: Vite build exits 0 and emits `frontend/dist` assets.

- [ ] **Step 2: Check the full diff for malformed markup**

Run: `git diff --check`

Expected: no whitespace errors.

- [ ] **Step 3: Commit any verification-driven correction**

```bash
git add frontend/src
git commit -m "chore: verify Windows 7 icon compatibility"
```
