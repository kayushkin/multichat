import { useEffect, useState } from 'react'

interface SchemaItem {
  name: string
  type: string
  sql: string
}

function Database() {
  const [schema, setSchema] = useState<SchemaItem[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

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

  useEffect(() => { fetchSchema() }, [])

  const tables = schema.filter(s => s.type === 'table')
  const indexes = schema.filter(s => s.type === 'index')

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-white">Database Schema</h1>
        <button
          onClick={fetchSchema}
          disabled={loading}
          className="bg-blue-600 hover:bg-blue-700 disabled:opacity-50 px-4 py-2 rounded-lg text-white transition-colors"
        >
          {loading ? 'Loading...' : 'Refresh'}
        </button>
      </div>

      {error && (
        <div className="bg-red-900/50 border border-red-700 rounded-lg p-4">
          <p className="text-red-300">{error}</p>
        </div>
      )}

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
    </div>
  )
}

export default Database
