function Inbox() {
  return (
    <div className="h-full flex">
      {/* Left Panel — Conversation list (populated from API) */}
      <div className="w-80 bg-gray-800 border-r border-gray-700 flex flex-col">
        <div className="p-4 border-b border-gray-700">
          <input
            type="text"
            placeholder="Search conversations..."
            className="w-full bg-gray-700 border border-gray-600 rounded-md px-3 py-2 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>
        <div className="flex-1 flex items-center justify-center">
          <p className="text-gray-500 text-sm px-4 text-center">
            No conversations yet. Connect a platform to see your messages.
          </p>
        </div>
      </div>

      {/* Right Panel — Message thread */}
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <div className="text-6xl mb-4">💬</div>
          <h3 className="text-xl text-white mb-2">No conversation selected</h3>
          <p className="text-gray-400">Connect a platform and select a conversation to start.</p>
        </div>
      </div>
    </div>
  )
}

export default Inbox
