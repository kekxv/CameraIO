# Final fix report: Element Plus plain frontend

Date: 2026-08-09

## Findings resolved

1. The Chrome 72 production runtime did not install `Promise.allSettled`, which is required by the migrated frontend. Added `es.promise.all-settled` to the Vite legacy runtime.
2. The Chrome 72 gate only inspected emitted syntax and CSS. It now locates the actual `polyfills-*.js`, verifies that `dist/index.html` loads it before the application entry, removes the host implementation, imports the emitted runtime, and checks fulfilled and rejected settlement semantics.
3. The camera device-information dialog derived visibility directly from its nullable backing object, allowing leave-transition renders to dereference `null`. Visibility now has a separate ref, content is guarded, and backing data is cleared only after `closed`.
4. The live snapshot dialog had the same lifecycle hazard and could clear/revoke snapshot state while the dialog still rendered. It now separates visibility from snapshot data, guards content, and revokes the object URL after the close transition; unmount cleanup remains immediate.
5. `Cameras.vue` still contained native/custom buttons, inputs, selects, and checkboxes after the migration. These controls are now Element Plus buttons, inputs, selects, radio groups, checkbox groups, and alerts while retaining the existing handlers, models, API calls, and camera media behavior.
6. `Live.vue` still contained native/custom camera-picker, grid, media, recording, and snapshot controls. These are now Element Plus controls while preserving the MJPEG URLs, native snapshot path, streaming handlers, recording handlers, and grid state.
7. Recording downloads nested Element Plus buttons inside anchors, creating invalid interactive markup, and the SSR fixture did not exercise representative table rows. Downloads now use `el-button tag="a"`; the fixture renders scoped rows and confirms visible time/size output.
8. The recording preview omitted its existing `gap` state from the migrated dialog, and several checkbox/radio values still used legacy `label` value semantics. The preview now overlays `该时间没有录像`; NVR channels, scanned devices, camera selection, schedule days, and radio options use explicit `value` semantics with the full metadata row as the checkbox affordance.

## TDD evidence

Focused RED command before production changes:

```sh
cd frontend
node --test --test-name-pattern='production Chrome 72 runtime|complete the Element Plus|dialog remains render-safe|valid download links' src/compat.test.mjs src/ui-smoke.test.mjs
```

Result: 0 passed, 5 failed for the intended gaps. The emitted-runtime probe failed with `Promise.allSettled was not installed`; the camera and live fragments failed on null `manufacturer` and `name` dereferences; the control/download/gap contracts also failed against the prior templates.

Focused GREEN evidence:

```sh
cd frontend
node --test --test-name-pattern='complete the Element Plus|dialog remains render-safe|valid download links|recording center renders Element Plus' src/ui-smoke.test.mjs
node --test --test-name-pattern='production Chrome 72 runtime' src/compat.test.mjs
```

Result: UI checks 5 passed, 0 failed; emitted Chrome 72 runtime check 1 passed, 0 failed.

## Final verification

| Command | Result |
|---|---|
| `cd frontend && npm test` | PASS — 47 passed, 0 failed |
| `cd frontend && npm run test:chrome72` | PASS — production build completed; emitted runtime semantic probe reported `Chrome 72 compatibility check passed` |
| `PATH=/tmp/cameraio-go-1.24.5/bin:/usr/local/go/bin:$PATH CGO_ENABLED=0 go test ./... -count=1` | PASS — all Go packages passed |
| `PATH=/tmp/cameraio-go-1.24.5/bin:/usr/local/go/bin:$PATH CGO_ENABLED=0 go vet ./...` | PASS |
| `bash scripts/tests/verify-single-host-recording-test.sh` | PASS — command contract passed |
| `bash -n scripts/verify-single-host-recording.sh` | PASS |
| Vue template compilation for `Cameras.vue`, `Live.vue`, and `Recordings.vue` | PASS |
| `git diff --check` | PASS |

Static final searches also found no native `button`, `input`, `select`, or `textarea` controls and no `ui-button`/`ui-icon-button` classes in the Cameras/Live templates; no Element Plus button retained invalid `type="button"`; Recordings retained no raw anchor wrapper.

## Commits

- `bfc19e6 fix: complete element plus plain migration` — production fixes and regression coverage.
- The documentation commit containing this report is recorded in the final handoff and can be resolved with `git log -1 -- .superpowers/sdd/2026-08-09-element-plus-plain-frontend/final-fix-report.md`.

## Warnings and unresolved environment evidence

- Vite still reports the pre-existing `@vueuse/core` PURE-comment notices and the existing greater-than-500 kB entry-chunk advisory; neither is a correctness failure introduced by these fixes.
- `verify-single-host-recording-test.sh` verifies the harness contract only. Full single-host acceptance still requires the documented camera, database, latency, 30-minute resource, disk, and playback-boundary evidence on the target deployment host.
- No functional review finding remains unresolved in this patch.
