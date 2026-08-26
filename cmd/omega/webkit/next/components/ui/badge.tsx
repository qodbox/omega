"use client"

import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { Slot } from "radix-ui"

import { cn } from "@/lib/utils"

const badgeVariants = cva(
  "group/badge inline-flex h-5 w-fit shrink-0 items-center justify-center gap-1 overflow-hidden border border-transparent px-1.5 py-0 text-xs font-medium whitespace-nowrap transition-colors duration-100 focus-visible:ring-2 focus-visible:ring-ring/50 has-data-[icon=inline-end]:pr-1 has-data-[icon=inline-start]:pl-1 aria-invalid:border-destructive [&>svg]:pointer-events-none [&>svg]:size-3!",
  {
    variants: {
      variant: {
        default: "bg-surface-gray-2 text-ink-gray-7 [a]:hover:bg-surface-gray-3",
        solid: "bg-surface-gray-10 text-ink-base [a]:hover:bg-surface-gray-9",
        secondary:
          "bg-surface-gray-2 text-ink-gray-7 [a]:hover:bg-surface-gray-3",
        brand: "bg-surface-brand-2 text-ink-brand [a]:hover:bg-surface-brand-3",
        success:
          "bg-surface-green-2 text-ink-green [a]:hover:bg-surface-green-3",
        warning:
          "bg-surface-amber-2 text-ink-amber [a]:hover:bg-surface-amber-3",
        info: "bg-surface-blue-2 text-ink-blue [a]:hover:bg-surface-blue-3",
        destructive: "bg-surface-red-2 text-ink-red [a]:hover:bg-surface-red-3",
        outline:
          "border-outline-gray-2 text-ink-gray-7 [a]:hover:bg-surface-gray-2",
        ghost: "text-ink-gray-6 [a]:hover:bg-surface-gray-2",
        link: "text-ink-link underline-offset-4 hover:underline",
      },
      shape: {
        default: "rounded-sm",
        pill: "rounded-full px-2",
        dot: "size-2 rounded-full p-0",
      },
    },
    defaultVariants: {
      variant: "default",
      shape: "default",
    },
  }
)

function Badge({
  className,
  variant = "default",
  shape = "default",
  asChild = false,
  ...props
}: React.ComponentProps<"span"> &
  VariantProps<typeof badgeVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : "span"

  return (
    <Comp
      data-slot="badge"
      data-variant={variant}
      className={cn(badgeVariants({ variant, shape }), className)}
      {...props}
    />
  )
}

export { Badge, badgeVariants }
