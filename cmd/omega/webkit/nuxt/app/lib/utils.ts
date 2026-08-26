import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

// Required by the components added with `bunx shadcn-vue add`.
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
