import { useEffect, useState } from 'react'
import PlatformIcon from '../components/PlatformIcon'

interface Contact {
  user_id: string
  display_name: string
  telegram_id: string
  phone?: string
  platform: string
  platforms?: string[]
}

interface Conversation {
  room_id: string
  name: string
  member_count: number
  platform: string
}

interface Stats {
  total_conversations: number
  active_today: number
  platforms_connected: number
  platform_breakdown: Array<{
    platform: string
    count: number
    percentage: number
  }>
}

function Dashboard() {
  const [contacts, setContacts] = useState<Contact[]>([])
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [stats, setStats] = useState<Stats | null>(null)
  const [error, setError] = useState('')
  const [scanning, setScanning] = useState(false)

  useEffect(() => {
    fetch('/api/contacts')
      .then(r => r.json())
      .then(data => setContacts(data || []))
      .catch(e => setError(e.message))

    fetch('/api/conversations')
      .then(r => r.json())
      .then(data => setConversations(data || []))
      .catch(e => setError(e.message))

    fetch('/api/stats')
      .then(r => r.json())
      .then(data => setStats(data || null))
      .catch(e => setError(e.message))
  }, [])

  const handleAIScan = async () => {
    setScanning(true)
    try {
      const response = await fetch('/api/ai/escalation-scan', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' }
      })
      if (response.ok) {
        const data = await response.json()
        // For now just show an alert, could be improved to show results
        alert(`AI Scan complete: ${data.total} escalations found`)
      }
    } catch (e) {
      alert('AI Scan failed')
    } finally {
      setScanning(false)
    }
  }

  const platforms = new Set(contacts.map(c => c.platform))
  const tgContacts = contacts.filter(c => c.platform === 'telegram')
  const waContacts = contacts.filter(c => c.platform === 'whatsapp')
  const sigContacts = contacts.filter(c => c.platform === 'signal')
  const fbContacts = contacts.filter(c => c.platform === 'facebook' || c.platform === 'meta')
  const igContacts = contacts.filter(c => c.platform === 'instagram')
  const imContacts = contacts.filter(c => c.platform === 'imessage')

  const statCards = [
    { label: 'Contacts', value: contacts.length },
    { label: 'Conversations', value: conversations.length },
    { label: 'Platforms', value: platforms.size },
  ]

  const platformRows = [
    { key: 'telegram', name: 'Telegram', contacts: tgContacts },
    { key: 'whatsapp', name: 'WhatsApp', contacts: waContacts },
    { key: 'signal', name: 'Signal', contacts: sigContacts },
    { key: 'facebook', name: 'Messenger', contacts: fbContacts },
    { key: 'instagram', name: 'Instagram', contacts: igContacts },
    { key: 'imessage', name: 'iMessage', contacts: imContacts },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-white">Dashboard</h1>
        <button
          onClick={handleAIScan}
          disabled={scanning}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-gray-900 font-medium transition-all disabled:opacity-50"
          style={{ backgroundColor: '#f5d40c' }}
        >
          {scanning ? (
            <span className="animate-spin inline-block w-4 h-4 border-2 border-gray-900 border-t-transparent rounded-full" />
          ) : (
            <span>🤖</span>
          )}
          {scanning ? 'Scanning...' : 'AI Scan'}
        </button>
      </div>

      {error && (
        <div className="bg-red-900/50 border border-red-700 rounded-lg p-4">
          <p className="text-red-300">{error}</p>
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        {statCards.map((stat) => (
          <div key={stat.label} className="bg-gray-800 rounded-lg p-6 border-l-4" style={{ borderLeftColor: '#f5d40c' }}>
            <p className="text-gray-400 text-sm">{stat.label}</p>
            <p className="text-2xl font-bold text-white mt-1">{stat.value}</p>
          </div>
        ))}
      </div>

      {/* Platform Chart Section */}
      {stats && stats.platform_breakdown && stats.platform_breakdown.length > 0 && (
        <div className="bg-gray-800 rounded-lg">
          <div className="p-6 border-b border-gray-700 border-l-4" style={{ borderLeftColor: '#f5d40c' }}>
            <h2 className="text-xl font-semibold text-white">Platform Breakdown</h2>
          </div>
          <div className="p-6">
            <div className="space-y-4">
              {stats.platform_breakdown.map((platform) => (
                <div key={platform.platform} className="flex items-center justify-between">
                  <div className="flex items-center gap-3">
                    <PlatformIcon platform={platform.platform} size={24} />
                    <span className="text-white font-medium capitalize">{platform.platform}</span>
                  </div>
                  <div className="flex items-center gap-4">
                    <div className="w-32 bg-gray-700 rounded-full h-2">
                      <div 
                        className="h-2 rounded-full" 
                        style={{ 
                          width: `${platform.percentage}%`,
                          backgroundColor: platform.platform === 'telegram' ? '#0088cc' :
                                           platform.platform === 'whatsapp' ? '#25D366' :
                                           platform.platform === 'signal' ? '#3A76F0' :
                                           platform.platform === 'facebook' ? '#1877f2' :
                                           platform.platform === 'instagram' ? '#E4405F' :
                                           '#6b7280'
                        }} 
                      />
                    </div>
                    <span className="text-gray-400 text-sm w-12 text-right">{platform.percentage}%</span>
                    <span className="text-white text-sm w-8 text-right">{platform.count}</span>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Platform Status */}
      <div className="bg-gray-800 rounded-lg">
        <div className="p-6 border-b border-gray-700 border-l-4" style={{ borderLeftColor: '#f5d40c' }}>
          <h2 className="text-xl font-semibold text-white">Platform Status</h2>
        </div>
        <div className="p-6 space-y-3">
          {platformRows.map((p) => (
            <div key={p.key} className="flex items-center justify-between p-3 rounded-lg bg-gray-700">
              <div className="flex items-center gap-3">
                <PlatformIcon platform={p.key} size={24} />
                <div>
                  <p className="text-white font-medium">{p.name}</p>
                  <p className="text-gray-400 text-sm">
                    {p.contacts.length > 0 ? `${p.contacts.length} contacts synced` : 'Not connected'}
                  </p>
                </div>
              </div>
              <div className={`w-3 h-3 rounded-full ${p.contacts.length > 0 ? 'bg-green-500' : 'bg-gray-500'}`} />
            </div>
          ))}
        </div>
      </div>

      {/* Contacts */}
      {contacts.length > 0 && (
        <div className="bg-gray-800 rounded-lg">
          <div className="p-6 border-b border-gray-700 border-l-4" style={{ borderLeftColor: '#f5d40c' }}>
            <h2 className="text-xl font-semibold text-white">Contacts ({contacts.length})</h2>
          </div>
          <div className="p-4 grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-3">
            {contacts.map((c) => (
              <div key={c.user_id} className="flex items-center gap-2 p-2 rounded-lg hover:bg-gray-700">
                <div className="w-8 h-8 bg-blue-600 rounded-full flex items-center justify-center text-xs text-white">
                  {c.display_name.charAt(0)}
                </div>
                <div className="min-w-0">
                  <p className="text-white text-sm truncate">{c.display_name}</p>
                  <p className="text-gray-500 text-xs flex items-center gap-1">
                    {(c.platforms || [c.platform]).map((p) => (
                      <PlatformIcon key={p} platform={p} size={12} />
                    ))}
                  </p>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

export default Dashboard
