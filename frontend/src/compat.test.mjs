import test from 'node:test'
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { pathToFileURL } from 'node:url'
import { parse } from 'postcss'

const readBuiltCSS = () => readdirSync('dist/assets')
  .filter((name) => name.endsWith('.css'))
  .map((name) => readFileSync(`dist/assets/${name}`, 'utf8'))
  .join('\n')

const rulesFor = (root, selector) => root.nodes
  .filter((node) => node.type === 'rule' && node.selector === selector)

const hasDeclaration = (rules, prop, value) => rules.some((rule) => rule.nodes
  .some((node) => node.type === 'decl' && node.prop === prop && (value === undefined || node.value === value)))

test('production bundle satisfies the Chrome 72 compatibility contract', () => {
  execFileSync('npm', ['run', 'build'], { stdio: 'inherit' })
  const result = execFileSync('node', ['scripts/assert-chrome72-build.mjs'], { encoding: 'utf8' })
  assert.match(result, /Chrome 72 compatibility check passed/)
})

test('the production Chrome 72 runtime installs a working Promise.allSettled', () => {
  execFileSync('npm', ['run', 'build'], { stdio: 'inherit' })
  const assetNames = readdirSync('dist/assets')
  const runtimeName = assetNames.find((name) => /^polyfills-.*\.js$/.test(name))
  assert.ok(runtimeName, 'the production build must emit the Chrome 72 runtime chunk')

  const indexHTML = readFileSync('dist/index.html', 'utf8')
  assert.ok(
    indexHTML.indexOf(runtimeName) < indexHTML.search(/assets\/index-.*\.js/),
    'the page must load the Chrome 72 runtime before the application entry',
  )

  const runtimeURL = pathToFileURL(resolve('dist/assets', runtimeName)).href
  const probe = `
    delete Promise.allSettled
    await import(${JSON.stringify(runtimeURL)})
    if (typeof Promise.allSettled !== 'function') throw new Error('Promise.allSettled was not installed')
    const values = await Promise.allSettled([Promise.resolve('ok'), Promise.reject(new Error('no'))])
    process.stdout.write(JSON.stringify(values.map(({ status, value, reason }) => ({
      status,
      value,
      reason: reason && reason.message,
    }))))
  `
  const result = execFileSync(process.execPath, ['--input-type=module', '--eval', probe], { encoding: 'utf8' })
  assert.deepEqual(JSON.parse(result), [
    { status: 'fulfilled', value: 'ok' },
    { status: 'rejected', reason: 'no' },
  ])
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

test('collapse headers activate their inherited flex-gap fallback in Chrome 72', () => {
  execFileSync('npm', ['run', 'build'], { stdio: 'inherit' })
  const root = parse(readBuiltCSS())
  const baseHeaderRules = rulesFor(root, '.el-collapse-item__header')
  const leftHeaderRules = rulesFor(root, '.el-collapse-icon-position-left .el-collapse-item__header')

  assert.ok(
    baseHeaderRules.some((rule) => rule.nodes
      .some((node) => node.type === 'decl' && node.prop === '--has-fgp' && node.value.trim() === '')),
    'the collapse header flex container must activate its generated fallback',
  )
  assert.ok(
    hasDeclaration(leftHeaderRules, 'gap')
      && hasDeclaration(leftHeaderRules, '--fgp-gap-row', '8px')
      && hasDeclaration(leftHeaderRules, '--fgp-gap-column', '8px'),
    'the left-positioned collapse header must retain its 8px fallback spacing',
  )
})

test('Element Plus flex-gap fallbacks do not disable interactive container hit areas', () => {
  execFileSync('npm', ['run', 'build'], { stdio: 'inherit' })
  const root = parse(readBuiltCSS())
  const polyfillPointerEvents = []

  root.walkDecls('pointer-events', (declaration) => {
    if (/^var\(--(?:parent-)?has-fgp\) (?:none|auto)$/.test(declaration.value)) {
      polyfillPointerEvents.push(`${declaration.parent.selector}: ${declaration.value}`)
    }
  })

  assert.deepEqual(polyfillPointerEvents, [])
})
