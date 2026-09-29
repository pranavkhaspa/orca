// A minimal PNG reader, in the repository, for the visual checks.
//
// The contrast and layout harnesses need to read the framebuffer, and the obvious
// answer — a dependency — is the wrong one here. The project has already had a
// CDN asset blocked and turn a hole in the interface; adding a package to read
// screenshots would be the same class of risk for a feature that is four
// functions long. Chrome only ever emits 8-bit RGBA or RGB, non-interlaced, and
// that is all this needs to handle. If it ever emits something else, it throws
// rather than returning wrong pixels.
import { inflateSync } from 'node:zlib'

const SIG = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])

function paeth(a, b, c) {
  const p = a + b - c
  const pa = Math.abs(p - a), pb = Math.abs(p - b), pc = Math.abs(p - c)
  if (pa <= pb && pa <= pc) return a
  return pb <= pc ? b : c
}

export const PNG = {
  decode(buf) {
    if (!buf.subarray(0, 8).equals(SIG)) throw new Error('not a PNG')
    let pos = 8
    let width = 0, height = 0, depth = 0, colorType = 0, interlace = 0
    const idat = []

    while (pos < buf.length) {
      const len = buf.readUInt32BE(pos)
      const type = buf.toString('ascii', pos + 4, pos + 8)
      const body = buf.subarray(pos + 8, pos + 8 + len)
      if (type === 'IHDR') {
        width = body.readUInt32BE(0)
        height = body.readUInt32BE(4)
        depth = body[8]
        colorType = body[9]
        interlace = body[12]
      } else if (type === 'IDAT') {
        idat.push(body)
      } else if (type === 'IEND') {
        break
      }
      pos += 12 + len
    }

    if (depth !== 8) throw new Error(`unsupported bit depth ${depth}`)
    if (interlace !== 0) throw new Error('interlaced PNG is not supported')
    const channels = colorType === 6 ? 4 : colorType === 2 ? 3 : 0
    if (channels === 0) throw new Error(`unsupported colour type ${colorType}`)

    // Un-filter, one scanline at a time. Each row is prefixed with its filter
    // byte, and most rows in a UI screenshot use the same filter as the row
    // above, which is what makes the encoded files small.
    const raw = inflateSync(Buffer.concat(idat))
    const stride = width * channels
    const out = Buffer.alloc(stride * height)
    let src = 0
    for (let y = 0; y < height; y++) {
      const filter = raw[src++]
      const row = y * stride
      const prev = row - stride
      for (let x = 0; x < stride; x++) {
        const v = raw[src + x]
        const a = x >= channels ? out[row + x - channels] : 0
        const b = y > 0 ? out[prev + x] : 0
        const c = x >= channels && y > 0 ? out[prev + x - channels] : 0
        let r
        switch (filter) {
          case 0: r = v; break
          case 1: r = v + a; break
          case 2: r = v + b; break
          case 3: r = v + ((a + b) >> 1); break
          case 4: r = v + paeth(a, b, c); break
          default: throw new Error(`unknown PNG filter ${filter}`)
        }
        out[row + x] = r & 0xff
      }
      src += stride
    }

    return { width, height, channels, data: out }
  },
}
