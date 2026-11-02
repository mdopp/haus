// Renders the web app's icons from the one master,
// internal/pages/symbol/icon.svg (owner, 2026-11-02): never draw or resize
// a PNG by hand. pages/symbol.go embeds and serves what this writes.
//
//   cd tools/icons && npm ci && node icons.mjs
//
// Writes into internal/pages/symbol/:
//   favicon.svg            the tab icon: the house on its ground, rounded
//   favicon-32.png         the same for browsers without SVG icons
//   apple-touch-icon.png   180 px, square and opaque: iOS rounds it itself
//   icon-192.png           the web app manifest's icons, rounded,
//   icon-512.png           transparent corners ("any")
//   icon-maskable-512.png  the whole 108 canvas: the launcher masks it ("maskable")
import { Resvg } from '@resvg/resvg-js'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const dir = path.join(root, 'internal/pages/symbol')
const master = fs.readFileSync(path.join(dir, 'icon.svg'), 'utf8')

/** [svg] seen through [box] (x y w h), clipped to [clip] (an SVG shape in the box's units) when given. */
function view(svg, box, clip) {
  let out = svg.replace(/<!--[\s\S]*?-->\s*/, '').replace('viewBox="0 0 108 108"', `viewBox="${box}"`)
  if (clip) {
    out = out
      .replace(/(<svg[^>]*>)/, `$1<clipPath id="clip">${clip}</clipPath><g clip-path="url(#clip)">`)
      .replace('</svg>', '</g></svg>')
  }
  return out
}

/** The visible 72 of the 108 canvas: what a launcher shows of an adaptive icon. */
const VISIBLE = '18 18 72 72'
const ROUNDED = '<rect x="18" y="18" width="72" height="72" rx="16"/>'

function png(svg, px) {
  return new Resvg(svg, { fitTo: { mode: 'width', value: px } }).render().asPng()
}

function write(name, bytes) {
  fs.writeFileSync(path.join(dir, name), bytes)
  console.log(path.relative(root, path.join(dir, name)))
}

const rounded = view(master, VISIBLE, ROUNDED)
write('favicon.svg', rounded.replace(/width="108" height="108"/, 'width="72" height="72"'))
write('favicon-32.png', png(rounded, 32))
write('apple-touch-icon.png', png(view(master, VISIBLE), 180))
write('icon-192.png', png(rounded, 192))
write('icon-512.png', png(rounded, 512))
write('icon-maskable-512.png', png(view(master, '0 0 108 108'), 512))
