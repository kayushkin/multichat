import { useState } from 'react'

interface SearchResult {
  room_id: string
  room_name: string
  platform: string
  sender: string
  sender_name: string
  body: string
  timestamp: number
  event_id: string
}

interface GroupedResult {
  room_id: string
  room_name: string
  platform: string
  results: SearchResult[]
  match_count: number
}

interface SearchResponse {
  query: string
  total: number
  groups: GroupedResult[]
}

function Search() {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const handleSearch = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!query.trim()) return

    setLoading(true)
    setError('')
    
    try {
      const response = await fetch(`/api/search?q=${encodeURIComponent(query.trim())}`)
      if (!response.ok) {
        throw new Error(`Search failed: ${response.statusText}`)
      }
      const data = await response.json()
      setResults(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Search failed')
    } finally {
      setLoading(false)
    }
  }

  const formatTime = (timestamp: number) => {
    return new Date(timestamp).toLocaleString()
  }

  const highlightMatch = (text: string, searchQuery: string) => {
    if (!searchQuery) return text
    
    const regex = new RegExp(`(${searchQuery})`, 'gi')
    const parts = text.split(regex)
    
    return parts.map((part, index) => 
      regex.test(part) ? (
        <mark key={index} className="bg-yellow-300 text-black px-1 rounded">
          {part}
        </mark>
      ) : part
    )
  }

  const getPlatformIcon = (platform: string) => {
    const icons: Record<string, string> = {
      telegram: '📱',
      whatsapp: '💬',
      facebook: '👤',
      instagram: '📷',
      signal: '🔒',
      unknown: '❓'
    }
    return icons[platform] || icons.unknown
  }

  const getPlatformColor = (platform: string) => {
    const colors: Record<string, string> = {
      telegram: 'bg-blue-500',
      whatsapp: 'bg-green-500', 
      facebook: 'bg-blue-600',
      instagram: 'bg-pink-500',
      signal: 'bg-gray-500',
      unknown: 'bg-gray-400'
    }
    return colors[platform] || colors.unknown
  }

  return (
    <div className="max-w-4xl mx-auto">
      <div className="mb-6">
        <h1 className="text-2xl font-bold mb-4">Search Conversations</h1>
        
        {/* Search Form */}
        <form onSubmit={handleSearch} className="mb-4">
          <div className="flex gap-2">
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search messages across all conversations..."
              className="flex-1 px-4 py-2 bg-gray-800 border border-gray-600 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 text-white"
            />
            <button
              type="submit"
              disabled={loading || !query.trim()}
              className="px-6 py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-600 disabled:cursor-not-allowed rounded-lg font-medium transition-colors"
            >
              {loading ? 'Searching...' : 'Search'}
            </button>
          </div>
        </form>

        {/* Search Stats */}
        {results && (
          <div className="text-sm text-gray-400 mb-4">
            Found {results.total} matches for "{results.query}" across {results.groups.length} conversations
          </div>
        )}
      </div>

      {/* Error */}
      {error && (
        <div className="bg-red-900/50 border border-red-600 rounded-lg p-4 mb-4">
          <p className="text-red-200">{error}</p>
        </div>
      )}

      {/* Results */}
      {results && results.groups.length > 0 && (
        <div className="space-y-6">
          {results.groups.map((group) => (
            <div key={group.room_id} className="bg-gray-800 rounded-lg border border-gray-700">
              {/* Conversation Header */}
              <div className="p-4 border-b border-gray-700">
                <div className="flex items-center justify-between">
                  <div className="flex items-center space-x-3">
                    <span className={`w-8 h-8 ${getPlatformColor(group.platform)} rounded-full flex items-center justify-center text-sm`}>
                      {getPlatformIcon(group.platform)}
                    </span>
                    <div>
                      <h3 className="font-semibold text-white">{group.room_name}</h3>
                      <p className="text-sm text-gray-400 capitalize">{group.platform}</p>
                    </div>
                  </div>
                  <span className="text-sm text-gray-400 bg-gray-700 px-2 py-1 rounded">
                    {group.match_count} {group.match_count === 1 ? 'match' : 'matches'}
                  </span>
                </div>
              </div>

              {/* Messages */}
              <div className="divide-y divide-gray-700">
                {group.results.slice(0, 5).map((result) => (
                  <div key={result.event_id} className="p-4 hover:bg-gray-700/50 transition-colors">
                    <div className="flex items-start space-x-3">
                      <div className="flex-1">
                        <div className="flex items-center space-x-2 mb-1">
                          <span className="text-sm font-medium text-blue-400">
                            {result.sender_name}
                          </span>
                          <span className="text-xs text-gray-500">
                            {formatTime(result.timestamp)}
                          </span>
                        </div>
                        <p className="text-gray-200 leading-relaxed">
                          {highlightMatch(result.body, results.query)}
                        </p>
                      </div>
                    </div>
                  </div>
                ))}
                
                {group.results.length > 5 && (
                  <div className="p-3 text-center">
                    <span className="text-sm text-gray-400">
                      +{group.results.length - 5} more matches in this conversation
                    </span>
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* No Results */}
      {results && results.groups.length === 0 && (
        <div className="text-center py-12">
          <div className="text-6xl mb-4">🔍</div>
          <h3 className="text-xl font-semibold mb-2">No results found</h3>
          <p className="text-gray-400">
            Try searching with different keywords or check your spelling
          </p>
        </div>
      )}

      {/* Initial State */}
      {!results && !loading && (
        <div className="text-center py-12">
          <div className="text-6xl mb-4">💬</div>
          <h3 className="text-xl font-semibold mb-2">Search your conversations</h3>
          <p className="text-gray-400">
            Search across all platforms and conversations for messages, names, or keywords
          </p>
        </div>
      )}
    </div>
  )
}

export default Search