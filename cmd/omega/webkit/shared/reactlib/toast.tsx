"use client"

import { useEffect, useState } from "react"
import { CircleCheckIcon, InfoIcon, OctagonXIcon, XIcon } from "lucide-react"

type Tone = "success" | "error" | "info"
type Entry = { id: number; message: string; detail?: string; tone: Tone }

let counter = 0
const entries: Entry[] = []
const listeners = new Set<(next: Entry[]) => void>()

function publish() {
  for (const listener of listeners) listener([...entries])
}

type Extra = { description?: string }

function push(message: string, tone: Tone, extra?: Extra) {
  const id = ++counter
  entries.push({ id, message, detail: extra?.description, tone })
  publish()
  setTimeout(() => dismiss(id), 5000)
}

function dismiss(id: number) {
  const at = entries.findIndex((entry) => entry.id === id)
  if (at >= 0) entries.splice(at, 1)
  publish()
}

export const toast = {
  success: (message: string, extra?: Extra) => push(message, "success", extra),
  error: (message: string, extra?: Extra) => push(message, "error", extra),
  info: (message: string, extra?: Extra) => push(message, "info", extra),
}

const icons = { success: CircleCheckIcon, error: OctagonXIcon, info: InfoIcon }
const tones = { success: "text-ink-green", error: "text-ink-red", info: "text-ink-blue" }

export function Toaster() {
  const [visible, setVisible] = useState<Entry[]>([])

  useEffect(() => {
    listeners.add(setVisible)
    return () => { listeners.delete(setVisible) }
  }, [])

  return (
    <div className="pointer-events-none fixed top-4 right-4 z-100 flex w-80 flex-col gap-2">
      {visible.map((entry) => {
        const Icon = icons[entry.tone]
        return (
          <div
            key={entry.id}
            className="pointer-events-auto flex items-start gap-2 rounded-lg border border-outline-gray-1 bg-popover p-3 text-base shadow-md"
          >
            <Icon className={`mt-0.5 size-4 shrink-0 ${tones[entry.tone]}`} />
            <div className="flex-1">
              <p className="text-p-sm text-ink-gray-8">{entry.message}</p>
              {entry.detail && <p className="text-p-xs text-ink-gray-5">{entry.detail}</p>}
            </div>
            <button
              type="button"
              className="text-ink-gray-4 hover:text-ink-gray-7"
              onClick={() => dismiss(entry.id)}
            >
              <XIcon className="size-4" />
            </button>
          </div>
        )
      })}
    </div>
  )
}
