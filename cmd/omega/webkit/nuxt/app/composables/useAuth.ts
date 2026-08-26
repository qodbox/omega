import { api, readTokens, subscribe, writeTokens, type User } from '~/lib/api'

const user = ref<User | null>(null)
const ready = ref(false)

export function useAuth() {
  // Same client, same storage, same token rotation as the other stacks:
  // only the way state is made reactive differs.
  async function resolve() {
    if (!readTokens()) {
      user.value = null
      ready.value = true
      return
    }

    user.value = await api.me().then(answer => answer.data).catch(() => null)
    if (!user.value) writeTokens(null)
    ready.value = true
  }

  async function restore() {
    if (ready.value) return
    await resolve()

    if (import.meta.client) {
      subscribe(() => { void resolve() })
      window.addEventListener('storage', () => { void resolve() })
    }
  }

  async function signIn(email: string, password: string) {
    const answer = await api.login(email, password)
    writeTokens(answer.tokens)
    user.value = answer.data
  }

  async function signUp(name: string, email: string, password: string) {
    const answer = await api.register(name, email, password)
    writeTokens(answer.tokens)
    user.value = answer.data
  }

  async function signOut() {
    await api.logout()
    user.value = null
  }

  return { user, ready, restore, signIn, signUp, signOut }
}
