import { useState } from 'react'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { useAuth } from './hooks/useAuth'
import Login from './pages/Login'
import ConversationList from './components/ConversationList'
import MessageThread from './components/MessageThread'
import ContactPanel from './components/ContactPanel'
import Dashboard from './components/Dashboard'
import { getConversation } from './api/chatwoot'

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 5000 } },
})

type View = 'inbox' | 'dashboard'

function AppInner() {
  const { login, logout, isAuthenticated, user } = useAuth()
  const [selectedConvo, setSelectedConvo] = useState<number | null>(null)
  const [platformFilter, setPlatformFilter] = useState('all')
  const [view, setView] = useState<View>('inbox')

  if (!isAuthenticated) return <Login onLogin={login} />

  return (
    <div className="h-screen flex flex-col" style={{ background: 'var(--bg-primary)' }}>
      {/* Top bar */}
      <header className="flex items-center justify-between px-5 py-3 shrink-0" style={{ background: 'var(--bg-secondary)', borderBottom: '1px solid var(--border)' }}>
        <div className="flex items-center gap-6">
          <h1 className="text-lg font-bold tracking-tight" style={{ color: 'var(--text-primary)' }}>
            <span style={{ color: 'var(--accent)' }}>Multi</span>Chat
          </h1>
          <nav className="flex gap-1">
            <NavBtn active={view === 'inbox'} onClick={() => setView('inbox')}>Inbox</NavBtn>
            <NavBtn active={view === 'dashboard'} onClick={() => setView('dashboard')}>Dashboard</NavBtn>
          </nav>
        </div>
        <div className="flex items-center gap-4">
          {view === 'inbox' && (
            <div className="flex gap-1 p-1 rounded-lg" style={{ background: 'var(--bg-tertiary)' }}>
              {['all', 'telegram', 'whatsapp'].map(p => (
                <button
                  key={p}
                  onClick={() => setPlatformFilter(p)}
                  className="px-3 py-1 rounded-md text-xs font-medium transition-colors cursor-pointer"
                  style={{
                    background: platformFilter === p ? 'var(--accent)' : 'transparent',
                    color: platformFilter === p ? 'white' : 'var(--text-secondary)',
                  }}
                >
                  {p === 'all' ? 'All' : p === 'telegram' ? '✈️ Telegram' : '💬 WhatsApp'}
                </button>
              ))}
            </div>
          )}
          <div className="flex items-center gap-2">
            <span className="text-xs" style={{ color: 'var(--text-secondary)' }}>{user?.name}</span>
            <button
              onClick={logout}
              className="text-xs px-2 py-1 rounded cursor-pointer"
              style={{ color: 'var(--text-muted)' }}
            >
              Logout
            </button>
          </div>
        </div>
      </header>

      {/* Main content */}
      {view === 'dashboard' ? (
        <Dashboard />
      ) : (
        <div className="flex-1 flex min-h-0">
          {/* Sidebar */}
          <div className="w-80 shrink-0 flex flex-col" style={{ borderRight: '1px solid var(--border)', background: 'var(--bg-secondary)' }}>
            <div className="px-4 py-3" style={{ borderBottom: '1px solid var(--border)' }}>
              <input
                type="text"
                placeholder="Search conversations..."
                className="w-full px-3 py-2 rounded-lg text-xs outline-none"
                style={{ background: 'var(--bg-tertiary)', color: 'var(--text-primary)', border: '1px solid var(--border)' }}
              />
            </div>
            <ConversationList selected={selectedConvo} onSelect={setSelectedConvo} platformFilter={platformFilter} />
          </div>

          {/* Message area */}
          <div className="flex-1 min-w-0">
            {selectedConvo ? (
              <MessageThread conversationId={selectedConvo} />
            ) : (
              <div className="flex flex-col items-center justify-center h-full">
                <div className="text-5xl mb-4">💬</div>
                <div className="text-sm font-medium" style={{ color: 'var(--text-secondary)' }}>
                  Select a conversation to get started
                </div>
              </div>
            )}
          </div>

          {/* Right panel */}
          <div className="w-72 shrink-0" style={{ borderLeft: '1px solid var(--border)', background: 'var(--bg-secondary)' }}>
            <SelectedContactPanel conversationId={selectedConvo} />
          </div>
        </div>
      )}
    </div>
  )
}

function SelectedContactPanel({ conversationId }: { conversationId: number | null }) {
  const { data } = useQuery({
    queryKey: ['conversation', conversationId],
    queryFn: () => getConversation(conversationId!),
    enabled: !!conversationId,
  })
  return <ContactPanel contact={data?.meta?.sender || null} />
}

function NavBtn({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className="px-3 py-1.5 rounded-lg text-sm font-medium transition-colors cursor-pointer"
      style={{
        background: active ? 'var(--bg-tertiary)' : 'transparent',
        color: active ? 'var(--text-primary)' : 'var(--text-secondary)',
      }}
    >
      {children}
    </button>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AppInner />
    </QueryClientProvider>
  )
}
