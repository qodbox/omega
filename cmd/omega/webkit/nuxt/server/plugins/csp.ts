import { createHash } from 'node:crypto'

import { contentSecurityPolicy } from '../../security'

// The SPA shell ships one inline script, the runtime config. Rather than
// allow every inline script, hash the ones this build emits and allow those
// alone. Runs when the shell is rendered: at `nuxt generate` time for the
// static output, per request when Nitro serves it.
const INLINE_SCRIPT = /<script(?![^>]*\ssrc=)([^>]*)>([\s\S]*?)<\/script>/g

function hashes(chunks: string[]) {
  const found = new Set<string>()
  for (const chunk of chunks) {
    for (const [, attributes = '', body = ''] of chunk.matchAll(INLINE_SCRIPT)) {
      // JSON payloads are data, never executed: no hash needed.
      if (/type=["']?application\/json/.test(attributes)) continue
      found.add(`sha256-${createHash('sha256').update(body).digest('base64')}`)
    }
  }
  return [...found]
}

export default defineNitroPlugin((nitro) => {
  if (import.meta.dev) return

  nitro.hooks.hook('render:html', (html, { event }) => {
    const scriptHashes = hashes([...html.head, ...html.bodyPrepend, ...html.body, ...html.bodyAppend])
    const apiUrl = useRuntimeConfig(event).public.apiBase as string

    // The header covers what a <meta> cannot (frame-ancestors) when Nitro
    // serves the page; the <meta> keeps the policy on any static host.
    setResponseHeader(event, 'Content-Security-Policy', contentSecurityPolicy({ scriptHashes, apiUrl }))
    const meta = contentSecurityPolicy({ scriptHashes, apiUrl, meta: true })
    html.head.unshift(`<meta http-equiv="Content-Security-Policy" content="${meta}">`)
  })
})
