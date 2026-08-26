export default defineNuxtRouteMiddleware(async () => {
  const { user, ready, restore } = useAuth()

  if (!ready.value) await restore()
  if (!user.value) return navigateTo('/login')
})
