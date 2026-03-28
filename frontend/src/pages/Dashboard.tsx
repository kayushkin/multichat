function Dashboard() {
  const stats = [
    { label: 'Total Messages', value: '1,234', icon: '💬' },
    { label: 'Active Conversations', value: '12', icon: '👥' },
    { label: 'Platforms Connected', value: '2', icon: '🔗' },
    { label: 'Unread Messages', value: '5', icon: '📮' },
  ]

  const recentActivity = [
    { platform: 'Telegram', message: 'New message from John Doe', time: '2 min ago', icon: '✈️' },
    { platform: 'WhatsApp', message: 'Group message in "Work Team"', time: '5 min ago', icon: '📱' },
    { platform: 'Telegram', message: 'Photo shared in "Family Group"', time: '12 min ago', icon: '✈️' },
    { platform: 'WhatsApp', message: 'Voice message from Alice', time: '20 min ago', icon: '📱' },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-white">Dashboard</h1>
        <button className="bg-blue-600 hover:bg-blue-700 px-4 py-2 rounded-lg text-white transition-colors">
          Sync All
        </button>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
        {stats.map((stat, index) => (
          <div key={index} className="bg-gray-800 rounded-lg p-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-gray-400 text-sm">{stat.label}</p>
                <p className="text-2xl font-bold text-white mt-1">{stat.value}</p>
              </div>
              <div className="text-2xl">{stat.icon}</div>
            </div>
          </div>
        ))}
      </div>

      {/* Recent Activity */}
      <div className="bg-gray-800 rounded-lg">
        <div className="p-6 border-b border-gray-700">
          <h2 className="text-xl font-semibold text-white">Recent Activity</h2>
        </div>
        <div className="p-6">
          <div className="space-y-4">
            {recentActivity.map((activity, index) => (
              <div key={index} className="flex items-start space-x-4 p-3 rounded-lg hover:bg-gray-700 transition-colors">
                <div className="text-xl">{activity.icon}</div>
                <div className="flex-1">
                  <div className="flex items-center justify-between">
                    <span className="text-blue-400 text-sm font-medium">{activity.platform}</span>
                    <span className="text-gray-400 text-xs">{activity.time}</span>
                  </div>
                  <p className="text-gray-300 mt-1">{activity.message}</p>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Platform Status */}
      <div className="bg-gray-800 rounded-lg">
        <div className="p-6 border-b border-gray-700">
          <h2 className="text-xl font-semibold text-white">Platform Status</h2>
        </div>
        <div className="p-6">
          <div className="space-y-4">
            <div className="flex items-center justify-between p-3 rounded-lg bg-gray-700">
              <div className="flex items-center space-x-3">
                <div className="text-xl">✈️</div>
                <div>
                  <p className="text-white font-medium">Telegram</p>
                  <p className="text-gray-400 text-sm">Connected</p>
                </div>
              </div>
              <div className="w-3 h-3 bg-green-500 rounded-full"></div>
            </div>
            <div className="flex items-center justify-between p-3 rounded-lg bg-gray-700">
              <div className="flex items-center space-x-3">
                <div className="text-xl">📱</div>
                <div>
                  <p className="text-white font-medium">WhatsApp</p>
                  <p className="text-gray-400 text-sm">Connected</p>
                </div>
              </div>
              <div className="w-3 h-3 bg-green-500 rounded-full"></div>
            </div>
            <div className="flex items-center justify-between p-3 rounded-lg bg-gray-700">
              <div className="flex items-center space-x-3">
                <div className="text-xl">🌐</div>
                <div>
                  <p className="text-white font-medium">Matrix</p>
                  <p className="text-gray-400 text-sm">Connecting...</p>
                </div>
              </div>
              <div className="w-3 h-3 bg-yellow-500 rounded-full"></div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export default Dashboard