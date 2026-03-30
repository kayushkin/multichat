import { useState, useEffect } from 'react'

interface AlertResult {
  room_id: string
  room_name: string
  platform: string
  sender: string
  sender_name: string
  body: string
  timestamp: number
  event_id: string
  keyword: string
  severity: string
}

interface AlertsResponse {
  alerts: AlertResult[]
  total: number
  hours_scanned: number
  keywords_used: string[]
  keywords_detail?: { keyword: string; severity: string }[]
  severity_counts: {
    high: number
    medium: number
    low: number
  }
  cutoff_time: number
}

function Alerts() {
  const [alerts, setAlerts] = useState<AlertsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [hoursFilter, setHoursFilter] = useState(24)
  const [customKeywords, setCustomKeywords] = useState('')

  const fetchAlerts = async () => {
    setLoading(true)
    setError('')
    
    try {
      const params = new URLSearchParams({
        hours: hoursFilter.toString()
      })
      if (customKeywords.trim()) {
        params.set('keywords', customKeywords.trim())
      }
      
      const response = await fetch(`/api/alerts?${params}`)
      if (!response.ok) {
        throw new Error(`Failed to fetch alerts: ${response.statusText}`)
      }
      const data = await response.json()
      setAlerts(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch alerts')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchAlerts()
  }, [hoursFilter])

  const handleRefresh = () => {
    fetchAlerts()
  }

  const formatTime = (timestamp: number) => {
    return new Date(timestamp).toLocaleString()
  }

  const getSeverityColor = (severity: string) => {
    const colors = {
      high: 'bg-red-500',
      medium: 'bg-yellow-500', 
      low: 'bg-blue-500'
    }
    return colors[severity as keyof typeof colors] || colors.low
  }

  const getSeverityIcon = (severity: string) => {
    const icons = {
      high: '🚨',
      medium: '⚠️',
      low: 'ℹ️'
    }
    return icons[severity as keyof typeof icons] || icons.low
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

  const highlightKeyword = (text: string, keyword: string) => {
    const regex = new RegExp(`(${keyword})`, 'gi')
    const parts = text.split(regex)
    
    return parts.map((part, index) => 
      regex.test(part) ? (
        <span key={index} className="bg-red-200 text-red-900 px-1 rounded font-medium">
          {part}
        </span>
      ) : part
    )
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <div className="text-lg text-gray-400">Loading alerts...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="bg-red-900/50 border border-red-600 rounded-lg p-4">
        <p className="text-red-200">{error}</p>
        <button 
          onClick={handleRefresh}
          className="mt-2 px-4 py-2 rounded-lg transition-colors text-gray-900 font-medium"
          style={{ backgroundColor: '#f5d40c' }}
        >
          Retry
        </button>
      </div>
    )
  }

  return (
    <div className="max-w-6xl mx-auto">
      {/* Header */}
      <div className="mb-6">
        <div className="flex items-center justify-between mb-4">
          <h1 className="text-2xl font-bold flex items-center space-x-2">
            <span style={{ color: '#f5d40c' }}>🚨</span>
            <span>Keyword Alerts</span>
          </h1>
          <button
            onClick={handleRefresh}
            className="px-4 py-2 rounded-lg transition-colors text-gray-900 font-medium"
            style={{ backgroundColor: '#f5d40c' }}
          >
            Refresh
          </button>
        </div>

        {/* Filters */}
        <div className="flex flex-wrap gap-4 items-end">
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1">
              Time Range
            </label>
            <select
              value={hoursFilter}
              onChange={(e) => setHoursFilter(parseInt(e.target.value))}
              className="px-3 py-2 bg-gray-800 border border-gray-600 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
            >
              <option value={1}>Last hour</option>
              <option value={6}>Last 6 hours</option>
              <option value={24}>Last 24 hours</option>
              <option value={72}>Last 3 days</option>
              <option value={168}>Last week</option>
            </select>
          </div>
          
          <div className="flex-1 max-w-md">
            <label className="block text-sm font-medium text-gray-300 mb-1">
              Custom Keywords (comma-separated)
            </label>
            <div className="flex gap-2">
              <input
                type="text"
                value={customKeywords}
                onChange={(e) => setCustomKeywords(e.target.value)}
                placeholder="e.g. priority, important, help"
                className="flex-1 px-3 py-2 bg-gray-800 border border-gray-600 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
              <button
                onClick={fetchAlerts}
                className="px-4 py-2 rounded-lg transition-colors text-gray-900 font-medium"
                style={{ backgroundColor: '#f5d40c' }}
              >
                Apply
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* Stats Summary */}
      {alerts && (
        <div className="grid grid-cols-1 md:grid-cols-4 gap-4 mb-6">
          <div className="bg-gray-800 rounded-lg p-4 border border-gray-700">
            <div className="text-2xl font-bold text-white">{alerts.total}</div>
            <div className="text-sm text-gray-400">Total Alerts</div>
          </div>
          <div className="bg-red-900/50 border border-red-600 rounded-lg p-4">
            <div className="text-2xl font-bold text-red-200 flex items-center space-x-2">
              <span>🚨</span>
              <span>{alerts.severity_counts.high}</span>
            </div>
            <div className="text-sm text-red-300">High Priority</div>
          </div>
          <div className="bg-yellow-900/50 border border-yellow-600 rounded-lg p-4">
            <div className="text-2xl font-bold text-yellow-200 flex items-center space-x-2">
              <span>⚠️</span>
              <span>{alerts.severity_counts.medium}</span>
            </div>
            <div className="text-sm text-yellow-300">Medium Priority</div>
          </div>
          <div className="bg-blue-900/50 border border-blue-600 rounded-lg p-4">
            <div className="text-2xl font-bold text-blue-200 flex items-center space-x-2">
              <span>ℹ️</span>
              <span>{alerts.severity_counts.low}</span>
            </div>
            <div className="text-sm text-blue-300">Low Priority</div>
          </div>
        </div>
      )}

      {/* Alerts List */}
      {alerts && alerts.alerts.length > 0 ? (
        <div className="space-y-3">
          {alerts.alerts.map((alert) => (
            <div 
              key={alert.event_id} 
              className="bg-gray-800 border border-gray-700 rounded-lg p-4 hover:bg-gray-750 transition-colors"
            >
              <div className="flex items-start space-x-4">
                {/* Severity indicator */}
                <div className={`w-3 h-3 rounded-full ${getSeverityColor(alert.severity)} mt-2`} />
                
                <div className="flex-1">
                  {/* Header */}
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center space-x-3">
                      <span className={`w-6 h-6 ${getPlatformColor(alert.platform)} rounded-full flex items-center justify-center text-xs`}>
                        {getPlatformIcon(alert.platform)}
                      </span>
                      <span className="font-medium text-white">{alert.room_name}</span>
                      <span className="text-sm text-gray-400">•</span>
                      <span className="text-sm text-blue-400">{alert.sender_name}</span>
                    </div>
                    
                    <div className="flex items-center space-x-2 text-sm text-gray-400">
                      <span>{getSeverityIcon(alert.severity)}</span>
                      <span className="capitalize">{alert.severity}</span>
                      <span>•</span>
                      <span>{formatTime(alert.timestamp)}</span>
                    </div>
                  </div>
                  
                  {/* Message content */}
                  <div className="text-gray-200 leading-relaxed mb-2">
                    {highlightKeyword(alert.body, alert.keyword)}
                  </div>
                  
                  {/* Keyword matched */}
                  <div className="text-xs text-gray-500">
                    Triggered by keyword: <span className="text-red-400 font-medium">"{alert.keyword}"</span>
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      ) : (
        /* No alerts */
        <div className="text-center py-12">
          <div className="text-6xl mb-4">✅</div>
          <h3 className="text-xl font-semibold mb-2">No alerts found</h3>
          <p className="text-gray-400">
            {alerts ? 
              `No problematic messages detected in the last ${alerts.hours_scanned} hours` :
              'No alerts to display'
            }
          </p>
        </div>
      )}

      {/* Keywords management */}
      {alerts && (
        <div className="mt-8">
          <div className="bg-gray-950 border border-gray-800 rounded-xl p-5">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-medium text-gray-400">Monitoring {alerts.keywords_used.length} keywords</h3>
              <form onSubmit={async (e) => {
                e.preventDefault()
                const input = (e.target as HTMLFormElement).querySelector('input') as HTMLInputElement
                const kw = input.value.trim()
                if (!kw) return
                await fetch('/api/alerts', {
                  method: 'POST',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify({ action: 'add', keyword: kw }),
                })
                input.value = ''
                fetchAlerts()
              }} className="flex gap-2">
                <input
                  type="text"
                  placeholder="Add keyword..."
                  className="bg-gray-900 border border-gray-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-gray-500 focus:outline-none focus:ring-1 w-40"
                />
                <button type="submit" className="px-3 py-1.5 rounded-lg text-sm font-medium text-gray-900" style={{ backgroundColor: '#f5d40c' }}>Add</button>
              </form>
            </div>
            <div className="flex flex-wrap gap-1.5">
              {(alerts.keywords_detail || alerts.keywords_used.map((k: string) => ({ keyword: k, severity: 'low' }))).map((kw: any, index: number) => {
                const sevColors: Record<string, string> = {
                  high: 'bg-red-900/30 text-red-300 border-red-700',
                  medium: 'bg-yellow-900/30 text-yellow-300 border-yellow-700',
                  low: 'bg-gray-800 text-gray-300 border-gray-700',
                }
                const sevIcons: Record<string, string> = { high: '🔴', medium: '🟡', low: '⚪' }
                const nextSev: Record<string, string> = { low: 'medium', medium: 'high', high: 'low' }
                return (
                  <span key={index} className={`text-xs px-2.5 py-1 rounded-full border flex items-center gap-1.5 ${sevColors[kw.severity] || sevColors.low}`}>
                    <button onClick={async () => {
                      await fetch('/api/alerts', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ action: 'set-severity', keyword: kw.keyword, severity: nextSev[kw.severity] || 'low' }),
                      })
                      fetchAlerts()
                    }} className="hover:opacity-70 transition-opacity" title={`Priority: ${kw.severity} (click to change)`}>
                      {sevIcons[kw.severity] || '⚪'}
                    </button>
                    {kw.keyword}
                    <button onClick={async () => {
                      await fetch('/api/alerts', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ action: 'remove', keyword: kw.keyword }),
                      })
                      fetchAlerts()
                    }} className="text-gray-500 hover:text-red-400 transition-colors">×</button>
                  </span>
                )
              })}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export default Alerts