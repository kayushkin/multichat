import { useState } from 'react'

function Inbox() {
  const [selectedPlatform, setSelectedPlatform] = useState('all')
  const [selectedConversation, setSelectedConversation] = useState<number | null>(null)

  const platforms = [
    { id: 'all', name: 'All Platforms', icon: '🌐' },
    { id: 'telegram', name: 'Telegram', icon: '✈️' },
    { id: 'whatsapp', name: 'WhatsApp', icon: '📱' },
    { id: 'matrix', name: 'Matrix', icon: '🔗' },
  ]

  const conversations = [
    { id: 1, name: 'John Doe', platform: 'telegram', lastMessage: 'Hey, how are you?', time: '2 min ago', unread: 2, avatar: '👨' },
    { id: 2, name: 'Work Team', platform: 'whatsapp', lastMessage: 'Meeting at 3 PM', time: '5 min ago', unread: 0, avatar: '👥' },
    { id: 3, name: 'Alice Smith', platform: 'telegram', lastMessage: 'Thanks for the file!', time: '12 min ago', unread: 1, avatar: '👩' },
    { id: 4, name: 'Family Group', platform: 'whatsapp', lastMessage: 'Photo', time: '20 min ago', unread: 3, avatar: '👨‍👩‍👧‍👦' },
    { id: 5, name: 'Dev Chat', platform: 'matrix', lastMessage: 'New deployment is ready', time: '1 hour ago', unread: 0, avatar: '💻' },
  ]

  const messages = selectedConversation ? [
    { id: 1, sender: 'John Doe', content: 'Hey, how are you?', time: '2 min ago', isMe: false },
    { id: 2, sender: 'Me', content: 'I\'m good, thanks! How about you?', time: '1 min ago', isMe: true },
    { id: 3, sender: 'John Doe', content: 'Doing well! Working on the new project.', time: 'Just now', isMe: false },
  ] : []

  const filteredConversations = selectedPlatform === 'all' 
    ? conversations 
    : conversations.filter(conv => conv.platform === selectedPlatform)

  return (
    <div className="h-full flex">
      {/* Left Panel - Platform Selector and Conversations */}
      <div className="w-80 bg-gray-800 border-r border-gray-700 flex flex-col">
        {/* Platform Selector */}
        <div className="p-4 border-b border-gray-700">
          <select 
            value={selectedPlatform}
            onChange={(e) => setSelectedPlatform(e.target.value)}
            className="w-full bg-gray-700 border border-gray-600 rounded-md px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            {platforms.map(platform => (
              <option key={platform.id} value={platform.id}>
                {platform.icon} {platform.name}
              </option>
            ))}
          </select>
        </div>

        {/* Conversations List */}
        <div className="flex-1 overflow-y-auto">
          {filteredConversations.map((conversation) => (
            <div
              key={conversation.id}
              onClick={() => setSelectedConversation(conversation.id)}
              className={`
                p-4 border-b border-gray-700 cursor-pointer transition-colors
                ${selectedConversation === conversation.id 
                  ? 'bg-blue-600' 
                  : 'hover:bg-gray-700'
                }
              `}
            >
              <div className="flex items-start space-x-3">
                <div className="text-2xl">{conversation.avatar}</div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center justify-between">
                    <h3 className="text-white font-medium truncate">{conversation.name}</h3>
                    <span className="text-xs text-gray-400">{conversation.time}</span>
                  </div>
                  <div className="flex items-center justify-between mt-1">
                    <p className="text-gray-300 text-sm truncate">{conversation.lastMessage}</p>
                    {conversation.unread > 0 && (
                      <span className="bg-blue-500 text-white text-xs rounded-full px-2 py-1 min-w-[20px] text-center">
                        {conversation.unread}
                      </span>
                    )}
                  </div>
                  <div className="flex items-center mt-1">
                    <span className="text-xs text-blue-400">
                      {platforms.find(p => p.id === conversation.platform)?.icon} 
                      {' '}
                      {platforms.find(p => p.id === conversation.platform)?.name}
                    </span>
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Right Panel - Messages */}
      <div className="flex-1 flex flex-col">
        {selectedConversation ? (
          <>
            {/* Chat Header */}
            <div className="bg-gray-800 border-b border-gray-700 p-4">
              <div className="flex items-center space-x-3">
                <div className="text-2xl">
                  {conversations.find(c => c.id === selectedConversation)?.avatar}
                </div>
                <div>
                  <h3 className="text-white font-medium">
                    {conversations.find(c => c.id === selectedConversation)?.name}
                  </h3>
                  <p className="text-gray-400 text-sm">
                    {platforms.find(p => p.id === conversations.find(c => c.id === selectedConversation)?.platform)?.name}
                  </p>
                </div>
              </div>
            </div>

            {/* Messages */}
            <div className="flex-1 p-4 overflow-y-auto space-y-4">
              {messages.map((message) => (
                <div
                  key={message.id}
                  className={`flex ${message.isMe ? 'justify-end' : 'justify-start'}`}
                >
                  <div
                    className={`
                      max-w-xs lg:max-w-md px-4 py-2 rounded-lg
                      ${message.isMe 
                        ? 'bg-blue-600 text-white' 
                        : 'bg-gray-700 text-white'
                      }
                    `}
                  >
                    {!message.isMe && (
                      <p className="text-xs text-gray-400 mb-1">{message.sender}</p>
                    )}
                    <p>{message.content}</p>
                    <p className="text-xs text-gray-400 mt-1">{message.time}</p>
                  </div>
                </div>
              ))}
            </div>

            {/* Message Input */}
            <div className="bg-gray-800 border-t border-gray-700 p-4">
              <div className="flex space-x-2">
                <input
                  type="text"
                  placeholder="Type a message..."
                  className="flex-1 bg-gray-700 border border-gray-600 rounded-md px-3 py-2 text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
                <button className="bg-blue-600 hover:bg-blue-700 px-4 py-2 rounded-md text-white transition-colors">
                  Send
                </button>
              </div>
            </div>
          </>
        ) : (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center">
              <div className="text-6xl mb-4">💬</div>
              <h3 className="text-xl text-white mb-2">Select a conversation</h3>
              <p className="text-gray-400">Choose a conversation from the list to start chatting</p>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

export default Inbox