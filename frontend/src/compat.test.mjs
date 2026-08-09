import test from 'node:test'
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { readFileSync, readdirSync } from 'node:fs'
import { parse } from 'postcss'

const readBuiltCSS = () => readdirSync('dist/assets')
  .filter((name) => name.endsWith('.css'))
  .map((name) => readFileSync(`dist/assets/${name}`, 'utf8'))
  .join('\n')

test('production bundle satisfies the Chrome 72 compatibility contract', () => {
  execFileSync('npm', ['run', 'build'], { stdio: 'inherit' })
  const result = execFileSync('node', ['scripts/assert-chrome72-build.mjs'], { encoding: 'utf8' })
  assert.match(result, /Chrome 72 compatibility check passed/)
})

test('Element Plus flex layouts include generated spacing fallbacks for Chrome 72', () => {
  execFileSync('npm', ['run', 'build'], { stdio: 'inherit' })
  const root = parse(readBuiltCSS())
  let elementFlexGapCount = 0

  root.walkDecls(/^(gap|row-gap|column-gap)$/, (declaration) => {
    if (!declaration.parent.selector?.includes('.el-')) return
    elementFlexGapCount += 1
    assert.match(
      declaration.value,
      /--fgp-(?:gap|row-gap|column-gap)/,
      `${declaration.parent.selector} retains an unsupported ${declaration.prop} declaration`,
    )
  })

  assert.ok(elementFlexGapCount > 0, 'expected Element Plus flex gap rules in the production CSS')
  assert.match(readBuiltCSS(), /--fgp-parent-gap-row/)
  assert.match(readBuiltCSS(), /margin-top:var\(--fgp-margin-top/)
  assert.match(readBuiltCSS(), /margin-left:var\(--fgp-margin-left/)
})
