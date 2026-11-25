// Create brotli variants of hashed JS/CSS assets after Vite build. The Go
// static server prefers them when the client sends Accept-Encoding: br; small
// files are skipped because the handshake costs more than the saved bytes.
import { readdir, readFile, writeFile } from 'node:fs/promises'
import { brotliCompressSync, constants } from 'node:zlib'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const assets = fileURLToPath(new URL('../../internal/web/dist/assets/', import.meta.url))
const entries = await readdir(assets, { withFileTypes: true })
let created = 0
for (const entry of entries) {
  if (!entry.isFile() || !/\.(js|css)$/.test(entry.name)) continue
  const source = join(assets, entry.name)
  const data = await readFile(source)
  if (data.length < 1024) continue
  const compressed = brotliCompressSync(data, {
    params: { [constants.BROTLI_PARAM_QUALITY]: 11 },
  })
  if (compressed.length >= data.length) continue
  await writeFile(source + '.br', compressed)
  created++
}
console.log(`brotli: ${created} assets precompressed`)
