import { useState, useEffect, useCallback, createContext, useContext } from 'react'
import * as api from '../api'

// Shared auth context for the entire app
const AuthContext = createContext(null)

export function AuthProvider({ children }) {
  const auth = useAuthState()
  return <AuthContext.Provider value={auth}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}

function useAuthState() {
  const [authed, setAuthed] = useState(!!api.getToken())
  const [tenant, setTenant] = useState(null)
  const [loading, setLoading] = useState(true)

  const loadTenant = useCallback(async () => {
    if (!api.getToken()) { setLoading(false); return }
    try {
      const t = await api.getMe()
      setTenant(t)
    } catch {
      // Token expired or invalid — clear auth state
      api.clearToken()
      setAuthed(false)
      setTenant(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (authed) loadTenant()
    else setLoading(false)
  }, [authed, loadTenant])

  const login = useCallback(async (email, password) => {
    const res = await api.login(email, password)
    api.setToken(res.token)
    setAuthed(true)
  }, [])

  const logout = useCallback(() => {
    api.clearToken()
    setAuthed(false)
    setTenant(null)
  }, [])

  return { authed, tenant, loading, login, logout, refreshTenant: loadTenant }
}
