export default defineNuxtRouteMiddleware(async (to) => {
  const { user, ready, restore } = useAuth()

  if (!ready.value) await restore()
  if (!user.value) return navigateTo({ path: '/login', query: { next: to.fullPath } })
})
