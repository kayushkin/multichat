import { useEffect, useState } from 'react'

interface SchemaItem {
  name: string
  type: string
  sql: string
}

interface UnifiedContact {
  display_name: string
  unified_id: string
  platforms: Array<{
    platform: string
    user_id: string
    avatar_url?: string
    phone?: string
    telegram_id?: string
  }>
}

function Database() {
  const [schema, setSchema] = useState<SchemaItem[]>([])
  const [unifiedContacts, setUnifiedContacts] = useState<UnifiedContact[]>([])
  const [activeTab, setActiveTab] = useState<'schema' | 'contacts'>('schema')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [contactsLoading, setContactsLoading] = useState(false)
  const [expandedContacts, setExpandedContacts] = useState<Set<string>>(new Set())

  const fetchSchema = async () => {
    setLoading(true)
    setError('')
    try {
      const res = await fetch('/api/schema')
      if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
      const data = await res.json()
      setSchema(data || [])
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  const fetchUnifiedContacts = async () => {
    setContactsLoading(true)
    setError('')
    try {
      const res = await fetch('/api/contacts/unified')
      if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
      const data = await res.json()
      setUnifiedContacts(data || [])
    } catch (e: any) {
      setError(e.message)
    } finally {
      setContactsLoading(false)
    }
  }

  const toggleContactExpansion = (unifiedId: string) => {
    const newExpanded = new Set(expandedContacts)
    if (newExpanded.has(unifiedId)) {
      newExpanded.delete(unifiedId)
    } else {
      newExpanded.add(unifiedId)
    }
    setExpandedContacts(newExpanded)
  }

  useEffect(() => { 
    fetchSchema()
    fetchUnifiedContacts()
  }, [])

  const tables = schema.filter(s => s.type === 'table')
  const indexes = schema.filter(s => s.type === 'index')

  const getPlatformBadgeColor = (platform: string) => {
    switch (platform) {
      case 'telegram': return 'bg-blue-500'
      case 'whatsapp': return 'bg-green-500'
      case 'facebook': return 'bg-blue-600'
      case 'instagram': return 'bg-purple-500'
      case 'meta': return 'bg-blue-600'
      case 'signal': return 'bg-indigo-500'
      default: return 'bg-gray-500'
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-white">Database</h1>
        <div className="flex gap-2">
          <button
            onClick={activeTab === 'schema' ? fetchSchema : fetchUnifiedContacts}
            disabled={loading || contactsLoading}
            className="bg-blue-600 hover:bg-blue-700 disabled:opacity-50 px-4 py-2 rounded-lg text-white transition-colors"
          >
            {(activeTab === 'schema' && loading) || (activeTab === 'contacts' && contactsLoading) ? 'Loading...' : 'Refresh'}
          </button>
        </div>
      </div>

      {/* Tabs */}
      <div className="border-b border-gray-700">
        <nav className="flex space-x-8">
          <button
            onClick={() => setActiveTab('schema')}
            className={`py-2 px-1 border-b-2 font-medium text-sm transition-colors ${
              activeTab === 'schema'
                ? 'border-blue-500 text-blue-400'
                : 'border-transparent text-gray-400 hover:text-gray-300 hover:border-gray-300'
            }`}
          >
            Schema
          </button>
          <button
            onClick={() => setActiveTab('contacts')}
            className={`py-2 px-1 border-b-2 font-medium text-sm transition-colors ${
              activeTab === 'contacts'
                ? 'border-blue-500 text-blue-400'
                : 'border-transparent text-gray-400 hover:text-gray-300 hover:border-gray-300'
            }`}
          >
            Unified Contacts
          </button>
        </nav>
      </div>

      {error && (
        <div className="bg-red-900/50 border border-red-700 rounded-lg p-4">
          <p className="text-red-300">{error}</p>
        </div>
      )}

      {activeTab === 'schema' ? (
        <>
          {/* Tables */}
      {tables.length > 0 && (
        <div className="space-y-4">
          <h2 className="text-lg font-semibold text-gray-300">
            Tables ({tables.length})
          </h2>
          {tables.map((item) => (
            <div key={item.name} className="bg-gray-800 rounded-lg overflow-hidden">
              <div className="px-4 py-3 border-b border-gray-700 flex items-center gap-3">
                <span className="text-xs font-mono bg-blue-600/30 text-blue-300 px-2 py-0.5 rounded">
                  TABLE
                </span>
                <span className="text-white font-medium">{item.name}</span>
              </div>
              <pre className="p-4 text-sm text-gray-300 font-mono overflow-x-auto whitespace-pre-wrap">
                {item.sql}
              </pre>
            </div>
          ))}
        </div>
      )}

      {/* Indexes */}
      {indexes.length > 0 && (
        <div className="space-y-4">
          <h2 className="text-lg font-semibold text-gray-300">
            Indexes ({indexes.length})
          </h2>
          {indexes.map((item) => (
            <div key={item.name} className="bg-gray-800 rounded-lg overflow-hidden">
              <div className="px-4 py-3 border-b border-gray-700 flex items-center gap-3">
                <span className="text-xs font-mono bg-green-600/30 text-green-300 px-2 py-0.5 rounded">
                  INDEX
                </span>
                <span className="text-white font-medium">{item.name}</span>
              </div>
              <pre className="p-4 text-sm text-gray-300 font-mono overflow-x-auto whitespace-pre-wrap">
                {item.sql}
              </pre>
            </div>
          ))}
        </div>
      )}

          {!loading && !error && schema.length === 0 && (
            <div className="bg-gray-800 rounded-lg p-8 text-center">
              <p className="text-gray-400">No schema found. Database may be empty.</p>
            </div>
          )}
        </>
      ) : (
        <>
          {/* Unified Contacts */}
          {contactsLoading ? (
            <div className="flex items-center justify-center py-8">
              <p className="text-gray-400">Loading unified contacts...</p>
            </div>
          ) : unifiedContacts.length === 0 ? (
            <div className="bg-gray-800 rounded-lg p-8 text-center">
              <p className="text-gray-400">No unified contacts found.</p>
            </div>
          ) : (
            <div className="space-y-4">
              <h2 className="text-lg font-semibold text-gray-300">
                Unified Contacts ({unifiedContacts.length})
              </h2>
              <div className="space-y-3">
                {unifiedContacts.map((contact) => (
                  <div key={contact.unified_id} className="bg-gray-800 rounded-lg overflow-hidden">
                    <div 
                      className="p-4 cursor-pointer hover:bg-gray-750 transition-colors"
                      onClick={() => toggleContactExpansion(contact.unified_id)}
                    >
                      <div className="flex items-center justify-between">
                        <div className="flex items-center gap-3">
                          <h3 className="text-white font-medium">{contact.display_name}</h3>
                          <div className="flex gap-2">
                            {contact.platforms.map((platform, idx) => (
                              <span
                                key={idx}
                                className={`text-xs px-2 py-1 rounded-full text-white ${getPlatformBadgeColor(platform.platform)}`}
                              >
                                {platform.platform}
                              </span>
                            ))}
                          </div>
                        </div>
                        <span className="text-gray-400 text-sm">
                          {expandedContacts.has(contact.unified_id) ? '▼' : '▶'}
                        </span>
                      </div>
                      <p className="text-gray-400 text-sm mt-1">
                        {contact.platforms.length} platform{contact.platforms.length !== 1 ? 's' : ''}
                      </p>
                    </div>
                    
                    {expandedContacts.has(contact.unified_id) && (
                      <div className="border-t border-gray-700 p-4 bg-gray-750">
                        <div className="space-y-3">
                          {contact.platforms.map((platform, idx) => (
                            <div key={idx} className="flex items-center justify-between p-3 bg-gray-700 rounded-lg">
                              <div className="flex items-center gap-3">
                                <span className={`w-3 h-3 rounded-full ${getPlatformBadgeColor(platform.platform)}`} />
                                <div>
                                  <p className="text-white font-medium capitalize">{platform.platform}</p>
                                  <p className="text-gray-400 text-sm font-mono">{platform.user_id}</p>
                                  {platform.phone && (
                                    <p className="text-gray-400 text-xs">Phone: {platform.phone}</p>
                                  )}
                                  {platform.telegram_id && (
                                    <p className="text-gray-400 text-xs">ID: {platform.telegram_id}</p>
                                  )}
                                </div>
                              </div>
                              {platform.avatar_url && (
                                <div className="text-gray-400 text-xs">
                                  📷 Avatar
                                </div>
                              )}
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}

export default Database
