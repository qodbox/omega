const collapsed = ref(false)
const mobileOpen = ref(false)

export function useSidebar() {
  function toggle() {
    collapsed.value = !collapsed.value
    if (import.meta.client) {
      localStorage.setItem('omega.sidebar', collapsed.value ? 'collapsed' : 'expanded')
    }
  }

  function restore() {
    if (!import.meta.client) return
    collapsed.value = localStorage.getItem('omega.sidebar') === 'collapsed'

    // Same shortcut as the shadcn sidebar, so nobody has to relearn it.
    window.addEventListener('keydown', (event) => {
      if (event.key.toLowerCase() !== 'b' || !(event.metaKey || event.ctrlKey)) return
      event.preventDefault()
      toggle()
    })
  }

  return { collapsed, mobileOpen, toggle, restore }
}
