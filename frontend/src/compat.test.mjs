import test from 'node:test'
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'

test('production bundle satisfies the Chrome 72 compatibility contract', () => {
  execFileSync('npm', ['run', 'build'], { stdio: 'inherit' })
  const result = execFileSync('node', ['scripts/assert-chrome72-build.mjs'], { encoding: 'utf8' })
  assert.match(result, /Chrome 72 compatibility check passed/)
})
