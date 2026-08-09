import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { parse } from 'acorn'
import { parse as parseCSS } from 'postcss'

const assetsDir = new URL('../dist/assets/', import.meta.url)
const assetNames = readdirSync(assetsDir)
const scripts = assetNames.filter((name) => name.endsWith('.js'))
const styles = assetNames.filter((name) => name.endsWith('.css'))

const findUnsupportedSyntax = (node) => {
  if (!node || typeof node !== 'object') return null
  if (node.type === 'ChainExpression') return '?.'
  if (node.type === 'PrivateIdentifier') return '#private'
  if (node.type === 'StaticBlock') return 'static {'
  if (node.type === 'LogicalExpression' && node.operator === '??') return '??'

  for (const value of Object.values(node)) {
    for (const child of Array.isArray(value) ? value : [value]) {
      const unsupportedSyntax = findUnsupportedSyntax(child)
      if (unsupportedSyntax) return unsupportedSyntax
    }
  }
  return null
}

for (const name of scripts) {
  const source = readFileSync(join(assetsDir.pathname, name), 'utf8')
  const ast = parse(source, { ecmaVersion: 2022, sourceType: 'module' })
  const unsupportedSyntax = findUnsupportedSyntax(ast)
  if (unsupportedSyntax) {
    throw new Error(`${name} contains Chrome 72-incompatible syntax: ${unsupportedSyntax}`)
  }
}

const css = styles.map((name) => readFileSync(join(assetsDir.pathname, name), 'utf8')).join('\n')
if (!css.includes('--chrome72-flex-gap-fallback')) {
  throw new Error('compiled CSS is missing the Flexbox gap fallback marker')
}
if (!/@supports not\s*\(aspect-ratio\s*:\s*1\s*\/\s*1\)/.test(css)) {
  throw new Error('compiled CSS is missing the aspect-ratio fallback')
}

let elementFlexGapCount = 0
const cssRoot = parseCSS(css)
cssRoot.walkDecls(/^(gap|row-gap|column-gap)$/, (declaration) => {
  if (!declaration.parent.selector?.includes('.el-')) return
  elementFlexGapCount += 1
  if (!/--fgp-(?:gap|row-gap|column-gap)/.test(declaration.value)) {
    throw new Error(`${declaration.parent.selector} retains a Chrome 72-incompatible ${declaration.prop} declaration`)
  }
})
if (elementFlexGapCount > 0 && (!css.includes('--fgp-parent-gap-row') || !css.includes('margin-top:var(--fgp-margin-top'))) {
  throw new Error('compiled Element Plus CSS is missing the Chrome 72 flex-gap fallback')
}

const collapseHeaderRules = cssRoot.nodes
  .filter((node) => node.type === 'rule' && node.selector === '.el-collapse-item__header')
const collapseFallbackActive = collapseHeaderRules.some((rule) => rule.nodes
  .some((node) => node.type === 'decl' && node.prop === '--has-fgp' && node.value.trim() === ''))
if (!collapseFallbackActive) {
  throw new Error('compiled collapse header CSS does not activate its Chrome 72 flex-gap fallback')
}

cssRoot.walkDecls('pointer-events', (declaration) => {
  if (/^var\(--(?:parent-)?has-fgp\) (?:none|auto)$/.test(declaration.value)) {
    throw new Error(`${declaration.parent.selector} disables an Element Plus interactive hit area`)
  }
})

console.log('Chrome 72 compatibility check passed')
