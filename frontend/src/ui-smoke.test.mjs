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
