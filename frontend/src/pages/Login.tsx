import { useState } from 'react'

const ITS_YELLOW = '#f5d40c'

function Login({ onLogin }: { onLogin: () => void }) {
  const [token, setToken] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token }),
      })
      if (!res.ok) {
        const data = await res.json()
        throw new Error(data.error || 'Invalid token')
      }
      // Cookie is set by the server, also store in localStorage as backup
      document.cookie = `multichat_token=${token}; path=/; max-age=${86400 * 30}; samesite=lax`
      onLogin()
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-black flex items-center justify-center">
      <div className="bg-gray-950 border border-gray-800 rounded-2xl p-8 w-full max-w-sm">
        <h1 className="text-2xl font-bold text-white text-center mb-1" style={{ color: ITS_YELLOW }}>MultiChat</h1>
        <p className="text-gray-500 text-sm text-center mb-6">Enter your access token to continue</p>

        <form onSubmit={submit} className="space-y-4">
          <input
            type="password"
            value={token}
            onChange={e => setToken(e.target.value)}
            placeholder="Access token"
            className="w-full bg-gray-900 border border-gray-700 rounded-lg px-4 py-3 text-white placeholder-gray-500 focus:outline-none focus:ring-2"
            style={{ '--tw-ring-color': ITS_YELLOW } as any}
            autoFocus
          />
          {error && <p className="text-red-400 text-sm">{error}</p>}
          <button
            type="submit"
            disabled={loading || !token}
            className="w-full py-3 rounded-lg font-medium text-gray-900 disabled:opacity-50 transition-colors"
            style={{ backgroundColor: ITS_YELLOW }}
          >
            {loading ? 'Authenticating...' : 'Sign In'}
          </button>
        </form>
      </div>
    </div>
  )
}

export default Login
