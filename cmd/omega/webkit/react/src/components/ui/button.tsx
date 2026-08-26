import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { Slot } from "radix-ui"

import { cn } from "@/lib/utils"

const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center whitespace-nowrap border border-transparent bg-clip-padding font-medium outline-none select-none transition-[color,background-color,border-color,box-shadow] duration-100 focus-visible:ring-2 focus-visible:ring-ring/60 focus-visible:ring-offset-1 focus-visible:ring-offset-background active:not-aria-[haspopup]:translate-y-px disabled:pointer-events-none disabled:opacity-55 aria-invalid:border-destructive aria-invalid:ring-2 aria-invalid:ring-destructive/25 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default:
          "bg-surface-gray-10 text-ink-base shadow-raised hover:bg-surface-gray-9 active:bg-surface-gray-8 aria-expanded:bg-surface-gray-8",
        brand:
          "bg-brand text-brand-foreground shadow-raised hover:bg-brand-7 active:bg-brand-8 aria-expanded:bg-brand-8 dark:hover:bg-brand-6 dark:active:bg-brand-7",
        secondary:
          "bg-surface-gray-2 text-ink-gray-8 hover:bg-surface-gray-3 active:bg-surface-gray-4 aria-expanded:bg-surface-gray-4",
        outline:
          "border-outline-gray-2 bg-surface-base text-ink-gray-8 shadow-2xs hover:border-outline-gray-3 hover:bg-surface-gray-1 active:bg-surface-gray-3 aria-expanded:border-outline-gray-3 aria-expanded:bg-surface-gray-3",
        ghost:
          "text-ink-gray-8 hover:bg-surface-gray-3 active:bg-surface-gray-4 aria-expanded:bg-surface-gray-4",
        destructive:
          "bg-surface-red-7 text-ink-base shadow-raised hover:bg-surface-red-6 active:bg-surface-red-7 focus-visible:ring-destructive/40 dark:text-ink-gray-9",
        "destructive-subtle":
          "bg-surface-red-2 text-ink-red hover:bg-surface-red-3 active:bg-surface-red-3 focus-visible:ring-destructive/40",
        link: "text-ink-link underline-offset-4 hover:underline",
      },
      size: {
        default:
          "h-8 gap-1.5 rounded-md px-2.5 text-base has-data-[icon=inline-end]:pr-2 has-data-[icon=inline-start]:pl-2",
        xs: "h-6 gap-1 rounded-sm px-1.5 text-xs in-data-[slot=button-group]:rounded-md has-data-[icon=inline-end]:pr-1 has-data-[icon=inline-start]:pl-1 [&_svg:not([class*='size-'])]:size-3.5",
        sm: "h-7 gap-1.5 rounded-sm px-2 text-sm in-data-[slot=button-group]:rounded-md has-data-[icon=inline-end]:pr-1.5 has-data-[icon=inline-start]:pl-1.5 [&_svg:not([class*='size-'])]:size-4",
        lg: "h-10 gap-2 rounded-lg px-3.5 text-md has-data-[icon=inline-end]:pr-3 has-data-[icon=inline-start]:pl-3 [&_svg:not([class*='size-'])]:size-5",
        icon: "size-8 rounded-md",
        "icon-xs":
          "size-6 rounded-sm in-data-[slot=button-group]:rounded-md [&_svg:not([class*='size-'])]:size-3.5",
        "icon-sm": "size-7 rounded-sm in-data-[slot=button-group]:rounded-md",
        "icon-lg": "size-10 rounded-lg [&_svg:not([class*='size-'])]:size-5",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

function Button({
  className,
  variant = "default",
  size = "default",
  asChild = false,
  ...props
}: React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
  }) {
  const Comp = asChild ? Slot.Root : "button"

  return (
    <Comp
      data-slot="button"
      data-variant={variant}
      data-size={size}
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

export { Button, buttonVariants }
