export type Toast = {
  id: number
  message: string
  tone: 'success' | 'error' | 'info'
}

const toasts = ref<Toast[]>([])
let counter = 0

function push(message: string, tone: Toast['tone']) {
  const id = ++counter
  toasts.value = [...toasts.value, { id, message, tone }]
  setTimeout(() => {
    toasts.value = toasts.value.filter(toast => toast.id !== id)
  }, 5000)
}

export function useToast() {
  return {
    toasts,
    success: (message: string) => push(message, 'success'),
    error: (message: string) => push(message, 'error'),
    info: (message: string) => push(message, 'info'),
    dismiss: (id: number) => {
      toasts.value = toasts.value.filter(toast => toast.id !== id)
    },
  }
}
