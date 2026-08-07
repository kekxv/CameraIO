import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

const assetsDir = new URL('../dist/assets/', import.meta.url)
const assetNames = readdirSync(assetsDir)
const scripts = assetNames.filter((name) => name.endsWith('.js'))
const styles = assetNames.filter((name) => name.endsWith('.css'))

for (const name of scripts) {
  const source = readFileSync(join(assetsDir.pathname, name), 'utf8')
  for (const unsupportedSyntax of ['?.', '??', '#private', 'static {']) {
    if (source.includes(unsupportedSyntax)) {
      throw new Error(`${name} contains Chrome 72-incompatible syntax: ${unsupportedSyntax}`)
    }
  }
}

const css = styles.map((name) => readFileSync(join(assetsDir.pathname, name), 'utf8')).join('\n')
if (!css.includes('--chrome72-flex-gap-fallback')) {
  throw new Error('compiled CSS is missing the Flexbox gap fallback marker')
}
if (!/@supports not\s*\(aspect-ratio\s*:\s*1\s*\/\s*1\)/.test(css)) {
  throw new Error('compiled CSS is missing the aspect-ratio fallback')
}

console.log('Chrome 72 compatibility check passed')
