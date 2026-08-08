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

test('application shell uses a light navigation surface and an accessible mobile toggle', () => {
  const layout = readFileSync(new URL('./components/Layout.vue', import.meta.url), 'utf8')
  assert.match(layout, /app-shell/)
  assert.match(layout, /aria-label="切换导航"/)
  assert.doesNotMatch(layout, /bg-slate-900/)
  assert.match(css, /\.layout-sidebar \{ top: 60px; right: auto;/)
})

test('camera management keeps operational actions and uses compatible layout primitives', () => {
  const cameras = readFileSync(new URL('./views/Cameras.vue', import.meta.url), 'utf8')
  assert.match(cameras, /handleScanLAN/)
  assert.match(cameras, /ui-card/)
  assert.match(cameras, /compat-flex-gap-/)
  assert.match(cameras, /ui-modal/)
})

test('live view keeps media controls and applies the Chrome 72 aspect fallback', () => {
  const live = readFileSync(new URL('./views/Live.vue', import.meta.url), 'utf8')
  assert.match(live, /startStream/)
  assert.match(live, /stopStream/)
  assert.match(live, /compat-aspect-video/)
  assert.match(live, /captureSnapshot/)
  assert.match(live, /ui-modal/)
})

test('live view lets operators select the cameras shown in the preview grid', () => {
  const live = readFileSync(new URL('./views/Live.vue', import.meta.url), 'utf8')
  assert.match(live, /选择摄像头/)
  assert.match(live, /selectedCameraIDs/)
  assert.match(live, /toggleCameraSelection/)
  assert.match(live, /clearCameraSelection/)
})

test('recordings view preserves recording and schedule actions in a responsive surface', () => {
  const recordings = readFileSync(new URL('./views/Recordings.vue', import.meta.url), 'utf8')
  assert.match(recordings, /stopRecording/)
  assert.match(recordings, /saveSchedule/)
  assert.match(recordings, /ui-card/)
  assert.match(recordings, /overflow-x-auto/)
})
