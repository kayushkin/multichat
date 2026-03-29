import { useEffect, useState } from 'react'

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

function Dashboard() {
  const [contacts, setContacts] = useState<Contact[]>([])
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    fetch('/api/contacts')
      .then(r => r.json())
      .then(data => setContacts(data || []))
      .catch(e => setError(e.message))

    fetch('/api/conversations')
      .then(r => r.json())
      .then(data => setConversations(data || []))
      .catch(e => setError(e.message))
  }, [])

  const platforms = new Set(contacts.map(c => c.platform))
  const tgContacts = contacts.filter(c => c.platform === 'telegram')
  const waContacts = contacts.filter(c => c.platform === 'whatsapp')
  const sigContacts = contacts.filter(c => c.platform === 'signal')

  const stats = [
    { label: 'Contacts', value: contacts.length },
    { label: 'Conversations', value: conversations.length },
    { label: 'Platforms', value: platforms.size },
  ]

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-white">Dashboard</h1>

      {error && (
        <div className="bg-red-900/50 border border-red-700 rounded-lg p-4">
          <p className="text-red-300">{error}</p>
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        {stats.map((stat) => (
          <div key={stat.label} className="bg-gray-800 rounded-lg p-6">
            <p className="text-gray-400 text-sm">{stat.label}</p>
            <p className="text-2xl font-bold text-white mt-1">{stat.value}</p>
          </div>
        ))}
      </div>

      {/* Platform Status */}
      <div className="bg-gray-800 rounded-lg">
        <div className="p-6 border-b border-gray-700">
          <h2 className="text-xl font-semibold text-white">Platform Status</h2>
        </div>
        <div className="p-6 space-y-3">
          <div className="flex items-center justify-between p-3 rounded-lg bg-gray-700">
            <div className="flex items-center gap-3">
              <span className="text-xl">✈️</span>
              <div>
                <p className="text-white font-medium">Telegram</p>
                <p className="text-gray-400 text-sm">
                  {tgContacts.length > 0 ? `${tgContacts.length} contacts synced` : 'Not connected'}
                </p>
              </div>
            </div>
            <div className={`w-3 h-3 rounded-full ${tgContacts.length > 0 ? 'bg-green-500' : 'bg-gray-500'}`} />
          </div>
          <div className="flex items-center justify-between p-3 rounded-lg bg-gray-700">
            <div className="flex items-center gap-3">
              <span className="text-xl">📱</span>
              <div>
                <p className="text-white font-medium">WhatsApp</p>
                <p className="text-gray-400 text-sm">
                  {waContacts.length > 0 ? `${waContacts.length} contacts synced` : 'Not connected'}
                </p>
              </div>
            </div>
            <div className={`w-3 h-3 rounded-full ${waContacts.length > 0 ? 'bg-green-500' : 'bg-gray-500'}`} />
          </div>
          <div className="flex items-center justify-between p-3 rounded-lg bg-gray-700">
            <div className="flex items-center gap-3">
              <span className="text-xl">🔒</span>
              <div>
                <p className="text-white font-medium">Signal</p>
                <p className="text-gray-400 text-sm">
                  {sigContacts.length > 0 ? `${sigContacts.length} contacts synced` : 'Not connected'}
                </p>
              </div>
            </div>
            <div className={`w-3 h-3 rounded-full ${sigContacts.length > 0 ? 'bg-green-500' : 'bg-gray-500'}`} />
          </div>
        </div>
      </div>

      {/* Contacts */}
      {contacts.length > 0 && (
        <div className="bg-gray-800 rounded-lg">
          <div className="p-6 border-b border-gray-700">
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
                  <p className="text-gray-500 text-xs">
                    {(c.platforms || [c.platform]).map(p => p === 'telegram' ? '✈️' : p === 'signal' ? '🔒' : '📱').join(' ')}
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
