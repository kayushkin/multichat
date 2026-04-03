import { useEffect, useState } from 'react'

interface Load {
  id: number
  reference_number: string
  shipper: string
  consignee: string
  origin_city: string
  origin_state: string
  destination_city: string
  destination_state: string
  status: string
  rate: number
  weight: number
  pickup_date: string
  delivery_date: string
  equipment_type: string
  attachment_status?: string // tentative or confirmed
}

interface LoadsPanelProps {
  conversationId: string
}

const statusColors: Record<string, { bg: string; text: string; label: string }> = {
  open: { bg: 'bg-emerald-500/20', text: 'text-emerald-400', label: 'Open' },
  in_transit: { bg: 'bg-blue-500/20', text: 'text-blue-400', label: 'In Transit' },
  delivered: { bg: 'bg-gray-500/20', text: 'text-gray-400', label: 'Delivered' },
  cancelled: { bg: 'bg-red-500/20', text: 'text-red-400', label: 'Cancelled' },
}

const equipmentLabels: Record<string, string> = {
  dry_van: 'Dry Van',
  reefer: 'Reefer',
  flatbed: 'Flatbed',
  power_only: 'Power Only',
}

function LoadsPanel({ conversationId }: LoadsPanelProps) {
  const [attachedLoads, setAttachedLoads] = useState<Load[]>([])
  const [showAttachModal, setShowAttachModal] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<Load[]>([])
  const [searching, setSearching] = useState(false)
  const [collapsed, setCollapsed] = useState(false)
  const [loading, setLoading] = useState(true)

  const fetchAttachedLoads = async () => {
    try {
      const res = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/loads`)
      if (res.ok) {
        setAttachedLoads(await res.json() || [])
      }
    } catch { /* skip */ }
    setLoading(false)
  }

  useEffect(() => {
    setLoading(true)
    fetchAttachedLoads()
  }, [conversationId])

  const searchLoads = async (q: string) => {
    if (!q.trim()) {
      // Show all open loads when search is empty
      try {
        const res = await fetch('/api/loads?status=open')
        if (res.ok) setSearchResults(await res.json() || [])
      } catch { /* skip */ }
      return
    }
    setSearching(true)
    try {
      const res = await fetch(`/api/loads/search?q=${encodeURIComponent(q)}`)
      if (res.ok) setSearchResults(await res.json() || [])
    } catch { /* skip */ }
    setSearching(false)
  }

  const attachLoad = async (loadId: number, attachmentStatus: string = 'tentative') => {
    try {
      const res = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/loads`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ load_id: loadId, attachment_status: attachmentStatus }),
      })
      if (res.ok) {
        await fetchAttachedLoads()
        setSearchResults(prev => prev.filter(l => l.id !== loadId))
      }
    } catch { /* skip */ }
  }

  const toggleAttachmentStatus = async (loadId: number, currentStatus: string) => {
    const newStatus = currentStatus === 'tentative' ? 'confirmed' : 'tentative'
    try {
      const res = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/loads`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ load_id: loadId, attachment_status: newStatus }),
      })
      if (res.ok) {
        setAttachedLoads(prev => prev.map(l => l.id === loadId ? { ...l, attachment_status: newStatus } : l))
      }
    } catch { /* skip */ }
  }

  const detachLoad = async (loadId: number) => {
    try {
      const res = await fetch(`/api/conversations/${encodeURIComponent(conversationId)}/loads/${loadId}`, {
        method: 'DELETE',
      })
      if (res.ok) {
        setAttachedLoads(prev => prev.filter(l => l.id !== loadId))
      }
    } catch { /* skip */ }
  }

  const openAttachModal = () => {
    setShowAttachModal(true)
    setSearchQuery('')
    searchLoads('') // Load open loads by default
  }

  if (loading) return null

  const attachedIDs = new Set(attachedLoads.map(l => l.id))
  const filteredResults = searchResults.filter(l => !attachedIDs.has(l.id))

  return (
    <>
      {/* Attached loads bar */}
      <div className="border-b border-gray-800 bg-black/20">
        <div className="px-4 py-2 flex items-center justify-between">
          <button
            onClick={() => setCollapsed(!collapsed)}
            className="flex items-center gap-2 text-sm text-gray-400 hover:text-white transition-colors"
          >
            <span className="text-xs">{collapsed ? '▶' : '▼'}</span>
            <span className="text-[10px] uppercase tracking-wider font-medium">
              Loads ({attachedLoads.length})
            </span>
          </button>
          <button
            onClick={openAttachModal}
            className="text-xs px-2.5 py-1 rounded-lg font-medium transition-colors bg-gray-800 text-gray-300 hover:bg-gray-700 border border-gray-700"
          >
            📦 Attach Load
          </button>
        </div>

        {!collapsed && attachedLoads.length > 0 && (
          <div className="px-4 pb-3 space-y-2">
            {attachedLoads.map(load => {
              const st = statusColors[load.status] || statusColors.open
              return (
                <div key={load.id} className="flex items-center gap-3 bg-gray-900/60 rounded-lg px-3 py-2 border border-gray-800">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-white text-sm font-mono font-medium">{load.reference_number}</span>
                      <span className={`text-[10px] px-1.5 py-0.5 rounded-full font-medium ${st.bg} ${st.text}`}>
                        {st.label}
                      </span>
                      <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-gray-800 text-gray-400">
                        {equipmentLabels[load.equipment_type] || load.equipment_type}
                      </span>
                      <button
                        onClick={(e) => { e.stopPropagation(); toggleAttachmentStatus(load.id, load.attachment_status || 'tentative') }}
                        className={`text-[10px] px-2 py-0.5 rounded-full font-medium border transition-colors ${
                          load.attachment_status === 'confirmed'
                            ? 'bg-emerald-500/20 text-emerald-400 border-emerald-500/40 hover:bg-emerald-500/30'
                            : 'bg-yellow-500/20 text-yellow-400 border-yellow-500/40 hover:bg-yellow-500/30'
                        }`}
                        title={`Click to mark as ${load.attachment_status === 'confirmed' ? 'tentative' : 'confirmed'}`}
                      >
                        {load.attachment_status === 'confirmed' ? '✓ Confirmed' : '⏳ Tentative'}
                      </button>
                    </div>
                    <div className="flex items-center gap-1 mt-0.5">
                      <span className="text-xs text-gray-400">
                        {load.origin_city}, {load.origin_state}
                      </span>
                      <span className="text-xs text-gray-600">→</span>
                      <span className="text-xs text-gray-400">
                        {load.destination_city}, {load.destination_state}
                      </span>
                    </div>
                    <div className="flex items-center gap-3 mt-0.5">
                      <span className="text-xs text-green-400 font-medium">${load.rate.toLocaleString()}</span>
                      <span className="text-[10px] text-gray-500">{load.shipper} → {load.consignee}</span>
                    </div>
                  </div>
                  <button
                    onClick={() => detachLoad(load.id)}
                    className="text-gray-600 hover:text-red-400 transition-colors text-sm flex-shrink-0"
                    title="Detach load"
                  >
                    ✕
                  </button>
                </div>
              )
            })}
          </div>
        )}

        {!collapsed && attachedLoads.length === 0 && (
          <div className="px-4 pb-3">
            <p className="text-xs text-gray-600 italic">No loads attached to this conversation</p>
          </div>
        )}
      </div>

      {/* Attach Load Modal */}
      {showAttachModal && (
        <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setShowAttachModal(false)}>
          <div className="bg-gray-950 rounded-lg w-[520px] max-h-[70vh] flex flex-col border border-gray-700" onClick={e => e.stopPropagation()}>
            <div className="p-4 border-b border-gray-700 flex items-center justify-between">
              <h3 className="text-white font-medium">📦 Attach Load</h3>
              <button onClick={() => setShowAttachModal(false)} className="text-gray-400 hover:text-white">✕</button>
            </div>
            <div className="p-3 border-b border-gray-700">
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => {
                  setSearchQuery(e.target.value)
                  searchLoads(e.target.value)
                }}
                placeholder="Search by reference, shipper, consignee, city..."
                autoFocus
                className="w-full bg-gray-800 border border-gray-600 rounded-lg px-3 py-2 text-white placeholder-gray-400 focus:outline-none focus:ring-1 focus:ring-blue-500 text-sm"
              />
            </div>
            <div className="flex-1 overflow-y-auto">
              {searching && <p className="text-gray-500 text-sm p-4 text-center">Searching...</p>}
              {!searching && filteredResults.length === 0 && (
                <p className="text-gray-500 text-sm p-4 text-center">
                  {searchQuery ? 'No loads found.' : 'No open loads available.'}
                </p>
              )}
              {filteredResults.map(load => {
                const st = statusColors[load.status] || statusColors.open
                return (
                  <div
                    key={load.id}
                    className="px-4 py-3 hover:bg-gray-800/50 transition-colors border-b border-gray-800/50"
                  >
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-white text-sm font-mono font-medium">{load.reference_number}</span>
                      <span className={`text-[10px] px-1.5 py-0.5 rounded-full font-medium ${st.bg} ${st.text}`}>
                        {st.label}
                      </span>
                      <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-gray-800 text-gray-400">
                        {equipmentLabels[load.equipment_type] || load.equipment_type}
                      </span>
                      <span className="text-xs text-green-400 font-medium ml-auto">${load.rate.toLocaleString()}</span>
                    </div>
                    <div className="flex items-center gap-1 mt-1">
                      <span className="text-xs text-gray-400">
                        {load.origin_city}, {load.origin_state}
                      </span>
                      <span className="text-xs text-gray-600">→</span>
                      <span className="text-xs text-gray-400">
                        {load.destination_city}, {load.destination_state}
                      </span>
                    </div>
                    <div className="flex items-center justify-between mt-1.5">
                      <div className="text-[10px] text-gray-500">
                        {load.shipper} → {load.consignee} · {load.weight?.toLocaleString()} lbs · Pickup {load.pickup_date}
                      </div>
                      <div className="flex gap-1.5 flex-shrink-0 ml-2">
                        <button
                          onClick={() => attachLoad(load.id, 'tentative')}
                          className="text-[10px] px-2.5 py-1 rounded font-medium border transition-colors bg-yellow-500/20 text-yellow-400 border-yellow-500/40 hover:bg-yellow-500/30"
                        >
                          ⏳ Tentative
                        </button>
                        <button
                          onClick={() => attachLoad(load.id, 'confirmed')}
                          className="text-[10px] px-2.5 py-1 rounded font-medium border transition-colors bg-emerald-500/20 text-emerald-400 border-emerald-500/40 hover:bg-emerald-500/30"
                        >
                          ✓ Confirmed
                        </button>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        </div>
      )}
    </>
  )
}

export default LoadsPanel
