import { useState, useEffect } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import Layout from './components/Layout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Chat from './pages/Chat'
import Database from './pages/Database'
import Search from './pages/Search'
import Alerts from './pages/Alerts'

function getToken(): string {
  const match = document.cookie.match(/(?:^|; )multichat_token=([^;]*)/)
  return match ? match[1] : ''
}

function App() {
  const [authed, setAuthed] = useState(false)
  const [checking, setChecking] = useState(true)

  useEffect(() => {
    // Check if existing token is valid
    const token = getToken()
    if (!token) {
      setChecking(false)
      return
    }
    fetch('/api/stats', {
      headers: { 'Authorization': `Bearer ${token}` }
    }).then(res => {
      if (res.ok) setAuthed(true)
    }).catch(() => {}).finally(() => setChecking(false))
  }, [])

  if (checking) return (
    <div className="min-h-screen bg-black flex items-center justify-center">
      <div className="animate-spin w-8 h-8 border-2 border-t-transparent rounded-full" style={{ borderColor: '#f5d40c', borderTopColor: 'transparent' }} />
    </div>
  )

  if (!authed) return <Login onLogin={() => setAuthed(true)} />

  return (
    <Routes>
      <Route path="/" element={<Layout />}>
        <Route index element={<Navigate to="/chat" replace />} />
        <Route path="dashboard" element={<Dashboard />} />
        <Route path="chat" element={<Chat />} />
        <Route path="search" element={<Search />} />
        <Route path="alerts" element={<Alerts />} />
        <Route path="database" element={<Database />} />
      </Route>
    </Routes>
  )
}

export default App
