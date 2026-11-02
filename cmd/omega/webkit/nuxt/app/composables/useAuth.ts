import { api, restoreSession, subscribe, type User } from '~/lib/api'

const user = ref<User | null>(null)
const ready = ref(false)
let wired = false

export function useAuth() {
  // Same client, same cookies, same rotation as the other stacks: only the
  // way state is made reactive differs.
  async function restore() {
    if (import.meta.client && !wired) {
      wired = true
      // Sign-in, sign-out and other tabs all go through the shared client.
      subscribe((next) => { user.value = next })
    }
    if (ready.value) return

    user.value = await restoreSession()
    ready.value = true
  }

  async function signIn(email: string, password: string) {
    user.value = await api.login(email, password)
  }

  async function signUp(name: string, email: string, password: string) {
    user.value = await api.register(name, email, password)
  }

  async function signOut() {
    await api.logout()
    user.value = null
  }

  return { user, ready, restore, signIn, signUp, signOut }
}
