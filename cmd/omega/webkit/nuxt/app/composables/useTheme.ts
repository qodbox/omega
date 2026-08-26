type Theme = 'light' | 'dark' | 'system'

const theme = ref<Theme>('system')

export function useTheme() {
  function apply() {
    if (!import.meta.client) return
    const dark = theme.value === 'dark'
      || (theme.value === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
    document.documentElement.classList.toggle('dark', dark)
  }

  function set(next: Theme) {
    theme.value = next
    localStorage.setItem('omega.theme', next)
    apply()
  }

  function restore() {
    if (!import.meta.client) return
    theme.value = (localStorage.getItem('omega.theme') as Theme | null) ?? 'system'
    apply()
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', apply)
  }

  return { theme, set, restore }
}
