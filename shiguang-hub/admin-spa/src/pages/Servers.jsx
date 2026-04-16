import { useState, useEffect } from 'react'
import { Plus, Server, X, AlertCircle, Pencil, Trash2 } from 'lucide-react'
import * as api from '../api'
import { Skeleton, EmptyState, ConfirmModal } from '../components'
import { useToast } from '../hooks/useToast'

// Default port presets per game version
const VERSION_PRESETS = {
  '5.8': { auth_port: 2108, game_port: 7777, chat_port: 10241 },
  '4.8': { auth_port: 2107, game_port: 7778, chat_port: 10241 },
}

const EMPTY_FORM = { name: '', version: '5.8', ...VERSION_PRESETS['5.8'] }

export default function ServersPage() {
  const [lines, setLines] = useState(null)
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState(null) // null = create mode, id = edit mode
  const [form, setForm] = useState(EMPTY_FORM)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState(null) // line to confirm delete
  const toast = useToast()

  const load = () => api.getLines().then(setLines).catch(() => setLines([]))
  useEffect(() => { load() }, [])

  const handleVersionChange = (v) => {
    setForm(prev => ({ ...prev, version: v, ...VERSION_PRESETS[v] }))
  }

  const openCreate = () => {
    setEditingId(null)
    setForm(EMPTY_FORM)
    setShowForm(true)
    setError('')
  }

  const openEdit = (line) => {
    setEditingId(line.id)
    setForm({
      name: line.name,
      version: line.version,
      auth_port: line.auth_port,
      game_port: line.game_port,
      chat_port: line.chat_port || 10241,
    })
    setShowForm(true)
    setError('')
  }

  const cancel = () => {
    setShowForm(false)
    setEditingId(null)
    setError('')
  }

  const submit = async (e) => {
    e.preventDefault()
    setError('')
    setSubmitting(true)
    try {
      if (editingId) {
        await api.updateLine(editingId, form)
        toast.success('Server line updated')
      } else {
        await api.createLine(form)
        toast.success('Server line created')
      }
      cancel()
      load()
    } catch (err) {
      setError(err.message)
    } finally {
      setSubmitting(false)
    }
  }

  const confirmDelete = async () => {
    if (!deleteTarget) return
    try {
      await api.deleteLine(deleteTarget.id)
      toast.success(`Server line "${deleteTarget.name}" deleted`)
      setDeleteTarget(null)
      load()
    } catch (err) {
      toast.error(err.message)
      setDeleteTarget(null)
    }
  }

  return (
    <div className="page-enter">
      <div className="page-header">
        <div>
          <div className="page-title">Server Lines</div>
          <div className="page-subtitle">Manage game server routing configurations</div>
        </div>
        <button
          className={`btn ${showForm ? 'btn-secondary' : 'btn-primary'}`}
          onClick={() => showForm ? cancel() : openCreate()}
        >
          {showForm ? <><X size={16} /> Cancel</> : <><Plus size={16} /> Add Line</>}
        </button>
      </div>

      {/* Create/Edit form — slide open */}
      {showForm && (
        <div className="card" style={{ animation: 'modal-enter 0.2s ease' }}>
          <div className="card-header">
            <span className="card-title">{editingId ? 'Edit Server Line' : 'New Server Line'}</span>
          </div>
          {error && <div className="error-msg"><AlertCircle /><span>{error}</span></div>}
          <form onSubmit={submit}>
            <div className="form-grid">
              <div className="form-field">
                <label className="form-label">Name</label>
                <input
                  value={form.name}
                  onChange={e => setForm({ ...form, name: e.target.value })}
                  placeholder="e.g. AionCore 5.8"
                  required
                  autoFocus
                />
              </div>
              <div className="form-field">
                <label className="form-label">Version</label>
                <select value={form.version} onChange={e => handleVersionChange(e.target.value)}>
                  <option value="5.8">5.8 (AionCore)</option>
                  <option value="4.8">4.8 (Beyond)</option>
                </select>
                <span className="form-hint">Ports auto-fill based on version</span>
              </div>
              <div className="form-field">
                <label className="form-label">Auth Port</label>
                <input
                  type="number"
                  value={form.auth_port}
                  onChange={e => setForm({ ...form, auth_port: +e.target.value })}
                  min={1} max={65535}
                />
              </div>
              <div className="form-field">
                <label className="form-label">Game Port</label>
                <input
                  type="number"
                  value={form.game_port}
                  onChange={e => setForm({ ...form, game_port: +e.target.value })}
                  min={1} max={65535}
                />
              </div>
            </div>
            <button className="btn btn-primary" type="submit" disabled={submitting}>
              {submitting
                ? (editingId ? 'Saving...' : 'Creating...')
                : (editingId ? 'Save Changes' : 'Create Server Line')}
            </button>
          </form>
        </div>
      )}

      {/* Lines table */}
      <div className="card">
        {lines === null ? (
          <Skeleton variant="row" count={3} />
        ) : lines.length === 0 ? (
          <EmptyState
            icon={<Server />}
            title="No server lines configured"
            description="Add a server line to define game server routing. Each line maps to a specific game version and port configuration."
            action={
              !showForm && (
                <button className="btn btn-primary" onClick={openCreate}>
                  <Plus size={16} /> Create First Line
                </button>
              )
            }
          />
        ) : (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Version</th>
                  <th>Auth Port</th>
                  <th>Game Port</th>
                  <th>Status</th>
                  <th style={{ width: 80 }}>Actions</th>
                </tr>
              </thead>
              <tbody>
                {lines.map(l => (
                  <tr key={l.id}>
                    <td style={{ fontWeight: 500 }}>{l.name}</td>
                    <td>
                      <span className={`badge badge-v${l.version?.replace('.', '')}`}>
                        v{l.version}
                      </span>
                    </td>
                    <td className="mono tabular">{l.auth_port}</td>
                    <td className="mono tabular">{l.game_port}</td>
                    <td>
                      <span className={`badge ${l.enabled !== false ? 'badge-online' : 'badge-offline'}`}>
                        {l.enabled !== false ? 'Active' : 'Disabled'}
                      </span>
                    </td>
                    <td>
                      <div style={{ display: 'flex', gap: 4 }}>
                        <button
                          className="btn btn-ghost"
                          style={{ padding: '4px 6px', minWidth: 0 }}
                          title="Edit"
                          onClick={() => openEdit(l)}
                        >
                          <Pencil size={14} />
                        </button>
                        <button
                          className="btn btn-ghost"
                          style={{ padding: '4px 6px', minWidth: 0, color: 'var(--sg-danger)' }}
                          title="Delete"
                          onClick={() => setDeleteTarget(l)}
                        >
                          <Trash2 size={14} />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Delete confirmation modal */}
      {deleteTarget && (
        <ConfirmModal
          title="Delete Server Line"
          message={`Are you sure you want to delete "${deleteTarget.name}"? This action cannot be undone. Any agents referencing this line will stop routing traffic.`}
          confirmLabel="Delete"
          danger
          onConfirm={confirmDelete}
          onCancel={() => setDeleteTarget(null)}
        />
      )}
    </div>
  )
}
