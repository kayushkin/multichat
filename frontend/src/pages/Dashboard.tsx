import { useEffect, useState } from 'react'

interface DashboardStats {
  total_conversations: number
  active_today: number
  platforms_connected: number
  platform_breakdown: Array<{
    platform: string
    count: number
    percentage: number
  }>
  most_active: Array<{
    name: string
    platform: string
    room_id: string
  }>
}

const platformColors: Record<string, string> = {
  telegram: 'bg-sky-500',
  whatsapp: 'bg-emerald-500',
  facebook: 'bg-blue-600',
  instagram: 'bg-pink-500',
  signal: 'bg-blue-400',
  unknown: 'bg-gray-500',
}

const platformIcons: Record<string, string> = {
  telegram: '✈️',
  whatsapp: '💬',
  facebook: '📘',
  instagram: '📷',
  signal: '🔒',
  unknown: '❓',
}

function Dashboard() {
  const [stats, setStats] = useState<DashboardStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    fetch('/api/stats')
      .then(res => { if (!res.ok) throw new Error(`${res.status}`); return res.json() })
      .then(setStats)
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  if (loading) return (
    <div className="flex items-center justify-center py-20">
      <div className="animate-spin w-8 h-8 border-2 border-blue-500 border-t-transparent rounded-full" />
    </div>
  )

  if (error) return (
    <div className="bg-red-900/30 border border-red-700 rounded-lg p-4">
      <p className="text-red-300">Error loading dashboard: {error}</p>
    </div>
  )

  if (!stats) return null

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-white">Dashboard</h1>
        <span className="text-sm text-gray-500">Live data from Matrix bridges</span>
      </div>

      {/* Stat Cards */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-5">
        <StatCard label="Total Conversations" value={stats.total_conversations} icon="💬" />
        <StatCard label="Active Today" value={stats.active_today} icon="🔥" />
        <StatCard label="Platforms Connected" value={stats.platforms_connected} icon="🔗" />
      </div>

      {/* Platform Breakdown */}
      <div className="bg-gray-800 rounded-xl border border-gray-700">
        <div className="p-5 border-b border-gray-700">
          <h2 className="text-lg font-semibold text-white">Platform Breakdown</h2>
        </div>
        <div className="p-5 space-y-4">
          {stats.platform_breakdown.map(p => (
            <div key={p.platform} className="space-y-1.5">
              <div className="flex justify-between items-center text-sm">
                <span className="text-white font-medium flex items-center gap-2">
                  <span>{platformIcons[p.platform] || '❓'}</span>
                  <span className="capitalize">{p.platform}</span>
                </span>
                <span className="text-gray-400 tabular-nums">{p.count} ({p.percentage}%)</span>
              </div>
              <div className="bg-gray-700/50 rounded-full h-2.5">
                <div
                  className={`h-2.5 rounded-full transition-all ${platformColors[p.platform] || 'bg-gray-500'}`}
                  style={{ width: `${Math.max(p.percentage, 3)}%` }}
                />
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Most Active */}
      <div className="bg-gray-800 rounded-xl border border-gray-700">
        <div className="p-5 border-b border-gray-700">
          <h2 className="text-lg font-semibold text-white">Recent Conversations</h2>
        </div>
        <div className="divide-y divide-gray-700/50">
          {stats.most_active?.map((conv, i) => (
            <div key={i} className="flex items-center gap-3 px-5 py-3 hover:bg-gray-700/30 transition-colors">
              <div className={`w-2.5 h-2.5 rounded-full flex-shrink-0 ${platformColors[conv.platform] || 'bg-gray-500'}`} />
              <span className="text-white font-medium truncate">{conv.name}</span>
              <span className="text-gray-500 text-xs capitalize ml-auto flex-shrink-0">{conv.platform}</span>
            </div>
          ))}
          {(!stats.most_active || stats.most_active.length === 0) && (
            <div className="p-5 text-center text-gray-500">No recent activity</div>
          )}
        </div>
      </div>
    </div>
  )
}

function StatCard({ label, value, icon }: { label: string; value: number; icon: string }) {
  return (
    <div className="bg-gray-800 rounded-xl border border-gray-700 p-5">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-gray-400 text-sm">{label}</p>
          <p className="text-3xl font-bold text-white mt-1 tabular-nums">{value}</p>
        </div>
        <span className="text-3xl">{icon}</span>
      </div>
    </div>
  )
}

export default Dashboard
