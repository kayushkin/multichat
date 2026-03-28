import { useState, useCallback } from 'react'
import { login as apiLogin } from '../api/chatwoot'

export function useAuth() {
  const [token, setToken] = useState(() => localStorage.getItem('chatwoot_token'))
  const [user, setUser] = useState(() => {
    const saved = localStorage.getItem('chatwoot_user')
    return saved ? JSON.parse(saved) : null
  })

  const login = useCallback(async (email: string, password: string) => {
    const res = await apiLogin(email, password)
    const { access_token, account_id, name, email: userEmail, avatar_url } = res.data
    localStorage.setItem('chatwoot_token', access_token)
    localStorage.setItem('chatwoot_account_id', String(account_id))
    const userData = { name, email: userEmail, avatar_url }
    localStorage.setItem('chatwoot_user', JSON.stringify(userData))
    setToken(access_token)
    setUser(userData)
  }, [])

  const logout = useCallback(() => {
    localStorage.removeItem('chatwoot_token')
    localStorage.removeItem('chatwoot_account_id')
    localStorage.removeItem('chatwoot_user')
    setToken(null)
    setUser(null)
  }, [])

  return { token, user, login, logout, isAuthenticated: !!token }
}
