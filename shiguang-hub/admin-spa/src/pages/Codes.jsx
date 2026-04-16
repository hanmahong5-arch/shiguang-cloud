import { useState, useEffect } from 'react'
import { Plus, Key, AlertCircle, Trash2 } from 'lucide-react'
import * as api from '../api'
import { Skeleton, EmptyState, CopyButton, ConfirmModal } from '../components'
import { useToast } from '../hooks/useToast'

export default function CodesPage() {
  const [codes, setCodes] = useState(null)
  const [newCode, setNewCode] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState(null)
  const toast = useToast()

  const load = () => api.getCodes().then(setCodes).catch(() => setCodes([]))
  useEffect(() => { load() }, [])

  const submit = async (e) => {
    e.preventDefault()
    setError('')
    setSubmitting(true)
    try {
      await api.createCode(newCode)
      toast.success(`Invite code "${newCode.toUpperCase()}" created`)
      setNewCode('')
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
      await api.deleteCode(deleteTarget.code)
      toast.success(`Invite code "${deleteTarget.code}" deleted`)
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
          <div className="page-title">Invite Codes</div>
          <div className="page-subtitle">Players enter these codes in the launcher to connect to your server</div>
        </div>
      </div>

      {/* Create code form */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">Create Invite Code</span>
        </div>
        {error && <div className="error-msg"><AlertCircle /><span>{error}</span></div>}
        <form onSubmit={submit} style={{ display: 'flex', gap: 10, alignItems: 'flex-end' }}>
          <div className="form-field" style={{ flex: 1, marginBottom: 0 }}>
            <label className="form-label">Code</label>
            <input
              value={newCode}
              onChange={e => setNewCode(e.target.value.toUpperCase().replace(/[^A-Z0-9-]/g, ''))}
              placeholder="e.g. MYSERVER"
              required
              minLength={4}
              maxLength={12}
              style={{ fontFamily: '"Cascadia Code", "Fira Code", monospace', letterSpacing: 2 }}
            />
          </div>
          <button className="btn btn-primary" type="submit" disabled={submitting} style={{ marginBottom: 0, height: 38 }}>
            <Plus size={16} />
            {submitting ? 'Creating...' : 'Create'}
          </button>
        </form>
        <div className="form-hint" style={{ marginTop: 8 }}>
          4-12 characters, uppercase letters, numbers, and dashes only
        </div>
      </div>

      {/* Code cards */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">Active Codes</span>
          {codes && <span style={{ fontSize: 12, color: 'var(--sg-text-tertiary)' }}>{codes.length} total</span>}
        </div>

        {codes === null ? (
          <Skeleton variant="row" count={3} />
        ) : codes.length === 0 ? (
          <EmptyState
            icon={<Key />}
            title="No invite codes yet"
            description="Create an invite code above. Players will enter this code in the launcher to find and connect to your server."
          />
        ) : (
          <div className="code-grid">
            {codes.map(c => (
              <div key={c.code} className="code-card">
                <div>
                  <div className="code-value">{c.code}</div>
                  <span className={`badge ${c.active ? 'badge-online' : 'badge-offline'}`} style={{ marginTop: 6 }}>
                    {c.active ? 'Active' : 'Inactive'}
                  </span>
                </div>
                <div className="code-status" style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                  <CopyButton text={c.code} label="Copy" />
                  <button
                    className="btn btn-ghost"
                    style={{ padding: '4px 6px', minWidth: 0, color: 'var(--sg-danger)' }}
                    title="Delete code"
                    onClick={() => setDeleteTarget(c)}
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Delete confirmation modal */}
      {deleteTarget && (
        <ConfirmModal
          title="Delete Invite Code"
          message={`Are you sure you want to delete the invite code "${deleteTarget.code}"? Players currently using this code will no longer be able to connect.`}
          confirmLabel="Delete"
          danger
          onConfirm={confirmDelete}
          onCancel={() => setDeleteTarget(null)}
        />
      )}
    </div>
  )
}
