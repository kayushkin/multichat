function Dashboard() {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-white">Dashboard</h1>

      {/* Stats — populated from API once platforms are connected */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
        {['Messages', 'Conversations', 'Platforms', 'Unread'].map((label) => (
          <div key={label} className="bg-gray-800 rounded-lg p-6">
            <p className="text-gray-400 text-sm">{label}</p>
            <p className="text-2xl font-bold text-white mt-1">—</p>
          </div>
        ))}
      </div>

      {/* Platform Status */}
      <div className="bg-gray-800 rounded-lg">
        <div className="p-6 border-b border-gray-700">
          <h2 className="text-xl font-semibold text-white">Platform Status</h2>
        </div>
        <div className="p-6">
          <p className="text-gray-400">No platforms connected. Connect Telegram or WhatsApp to get started.</p>
        </div>
      </div>
    </div>
  )
}

export default Dashboard
