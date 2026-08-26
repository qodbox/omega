"use client"

import * as React from "react"

import { cn } from "@/lib/utils"

function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(
        "flex field-sizing-content min-h-16 w-full rounded-md border border-outline-gray-2 bg-surface-base px-2.5 py-1.5 text-base text-ink-gray-9 shadow-2xs transition-[color,border-color,box-shadow] duration-100 outline-none placeholder:text-ink-gray-4 hover:border-outline-gray-3 focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/35 disabled:cursor-not-allowed disabled:bg-surface-gray-2 disabled:text-ink-gray-4 aria-invalid:border-destructive aria-invalid:ring-2 aria-invalid:ring-destructive/25",
        className
      )}
      {...props}
    />
  )
}

export { Textarea }
