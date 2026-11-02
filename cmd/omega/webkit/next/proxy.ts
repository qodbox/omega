import { NextResponse, type NextRequest } from "next/server"

import { contentSecurityPolicy } from "./security"

// A nonce per request: Next stamps it on every script it renders, and any
// script an injection manages to slip into the page lacks it.
export function proxy(request: NextRequest) {
  const nonce = Buffer.from(crypto.randomUUID()).toString("base64")
  const policy = contentSecurityPolicy({
    nonce,
    apiUrl: process.env.NEXT_PUBLIC_API_URL,
    dev: process.env.NODE_ENV === "development",
  })

  const forwarded = new Headers(request.headers)
  forwarded.set("x-nonce", nonce)
  forwarded.set("Content-Security-Policy", policy)

  const response = NextResponse.next({ request: { headers: forwarded } })
  response.headers.set("Content-Security-Policy", policy)
  return response
}

export const config = {
  matcher: [
    {
      // Pages only: the API, static chunks and prefetches need no policy.
      source: "/((?!api|graphql|_next/static|_next/image|favicon.ico).*)",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
  ],
}
