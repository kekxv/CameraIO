import 'core-js/modules/es.array.flat.js'
import 'core-js/modules/es.array.flat-map.js'
import 'core-js/modules/es.object.from-entries.js'

// Chrome 72 supports `gap` for Grid but not for Flexbox, so CSS @supports is
// insufficient. Leave margin spacing enabled by default and opt into Flexbox
// gaps only after an actual layout check.
const supportsFlexGap = () => {
  const flex = document.createElement('div')
  const firstChild = document.createElement('div')
  const secondChild = document.createElement('div')
  flex.style.display = 'flex'
  flex.style.flexDirection = 'column'
  flex.style.rowGap = '1px'
  flex.appendChild(firstChild)
  flex.appendChild(secondChild)
  const container = document.body || document.documentElement
  container.appendChild(flex)
  const supported = flex.scrollHeight === 1
  container.removeChild(flex)
  return supported
}

if (supportsFlexGap()) document.documentElement.classList.add('has-flex-gap')
