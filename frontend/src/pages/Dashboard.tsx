import { useEffect, useState } from 'react'

interface DashboardStats {
  total_conversations: {
    telegram: number
    whatsapp: number
    facebook: number
    instagram: number
    total: number
  }
  platform_breakdown: Array<{
    platform: string
    count: number
    percentage: number
  }>
  most_active: Array<{
    name: string
    platform: string
    messages: number
  }>
}

function Dashboard() {
  const [stats, setStats] = useState<DashboardStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const fetchStats = async () => {
    setLoading(true)
    setError('')
    try {
      const res = await fetch('/api/stats')
      if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
      const data = await res.json()
      setStats(data)
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchStats() }, [])

  if (loading) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold text-white">Dashboard</h1>
        <div className="flex items-center justify-center py-12">
          <p className="text-gray-400">Loading dashboard...</p>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold text-white">Dashboard</h1>
        <div className="bg-red-900/50 border border-red-700 rounded-lg p-4">
          <p className="text-red-300">Error loading dashboard: {error}</p>
        </div>
      </div>
    )
  }

  if (!stats) return null

  const statCards = [
    { label: 'Total Conversations', value: stats.total_conversations.total },
    { label: 'Telegram', value: stats.total_conversations.telegram },
    { label: 'WhatsApp', value: stats.total_conversations.whatsapp },
    { label: 'Facebook', value: stats.total_conversations.facebook + stats.total_conversations.instagram },
  ]

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-white">Dashboard</h1>

      {/* Stats Cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
        {statCards.map((stat) => (
          <div key={stat.label} className="bg-gray-800 rounded-lg p-6">
            <p className="text-gray-400 text-sm">{stat.label}</p>
            <p className="text-2xl font-bold text-white mt-1">{stat.value}</p>
          </div>
        ))}
      </div>

      {/* Platform Breakdown */}
      <div className="bg-gray-800 rounded-lg">
        <div className="p-6 border-b border-gray-700">
          <h2 className="text-xl font-semibold text-white">Platform Breakdown</h2>
        </div>
        <div className="p-6 space-y-4">
          {stats.platform_breakdown.map((platform) => (
            <div key={platform.platform} className="space-y-2">
              <div className="flex justify-between text-sm">
                <span className="text-gray-300 capitalize">{platform.platform}</span>
                <span className="text-gray-400">{platform.count} conversations ({platform.percentage}%)</span>
              </div>
              <div className="bg-gray-700 rounded-full h-2">
                <div
                  className={`h-2 rounded-full ${
                    platform.platform === 'telegram' ? 'bg-blue-500' :
                    platform.platform === 'whatsapp' ? 'bg-green-500' :
                    platform.platform === 'facebook' ? 'bg-blue-600' :
                    'bg-purple-500'
                  }`}
                  style={{ width: `${platform.percentage}%` }}
                />
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Most Active Conversations */}
      <div className="bg-gray-800 rounded-lg">
        <div className="p-6 border-b border-gray-700">
          <h2 className="text-xl font-semibold text-white">Most Active Conversations</h2>
        </div>
        <div className="p-6">
          <div className="space-y-3">
            {stats.most_active.map((conv, index) => (
              <div key={index} className="flex items-center justify-between py-2">
                <div className="flex items-center gap-3">
                  <div className={`w-2 h-2 rounded-full ${
                    conv.platform === 'telegram' ? 'bg-blue-500' :
                    conv.platform === 'whatsapp' ? 'bg-green-500' :
                    conv.platform === 'facebook' ? 'bg-blue-600' :
                    'bg-purple-500'
                  }`} />
                  <span className="text-white font-medium">{conv.name}</span>
                  <span className="text-gray-400 text-sm capitalize">({conv.platform})</span>
                </div>
                <span className="text-gray-300 text-sm">{conv.messages} messages</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}

export default Dashboard
