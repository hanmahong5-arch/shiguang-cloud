import { useState } from 'react'
import { Lock, LogOut } from 'lucide-react'
import * as api from '../api'
import { useToast } from '../hooks/useToast'

export default function SettingsPage({ tenant, onLogout }) {
  const toast = useToast()
  const [oldPw, setOldPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirmPw, setConfirmPw] = useState('')
  const [saving, setSaving] = useState(false)

  const handleChangePassword = async (e) => {
    e.preventDefault()
    if (newPw.length < 8) {
      toast.error('New password must be at least 8 characters')
      return
    }
    if (newPw !== confirmPw) {
      toast.error('New passwords do not match')
      return
    }
    setSaving(true)
    try {
      await api.changePassword(oldPw, newPw)
      toast.success('Password changed successfully')
      setOldPw('')
      setNewPw('')
      setConfirmPw('')
    } catch (err) {
      toast.error(err.message)
    }
    setSaving(false)
  }

  return (
    <div className="page-enter">
      <div className="page-header">
        <div>
          <div className="page-title">Settings</div>
          <div className="page-subtitle">Manage your account security</div>
        </div>
      </div>

      {/* Password change */}
      <div className="card">
        <div className="card-header">
          <span className="card-title"><Lock size={14} style={{ marginRight: 6, verticalAlign: -2 }} />Change Password</span>
        </div>
        <form onSubmit={handleChangePassword} style={{ padding: 20, display: 'flex', flexDirection: 'column', gap: 14, maxWidth: 400 }}>
          <div className="form-field">
            <label className="form-label">Current Password</label>
            <input
              type="password"
              className="form-input"
              value={oldPw}
              onChange={e => setOldPw(e.target.value)}
              required
              autoComplete="current-password"
            />
          </div>
          <div className="form-field">
            <label className="form-label">New Password</label>
            <input
              type="password"
              className="form-input"
              value={newPw}
              onChange={e => setNewPw(e.target.value)}
              required
              minLength={8}
              autoComplete="new-password"
            />
            <span style={{ fontSize: 11, color: 'var(--sg-text-tertiary)' }}>Minimum 8 characters</span>
          </div>
          <div className="form-field">
            <label className="form-label">Confirm New Password</label>
            <input
              type="password"
              className="form-input"
              value={confirmPw}
              onChange={e => setConfirmPw(e.target.value)}
              required
              autoComplete="new-password"
            />
          </div>
          <button type="submit" className="btn btn-primary" disabled={saving} style={{ alignSelf: 'flex-start' }}>
            {saving ? 'Saving...' : 'Update Password'}
          </button>
        </form>
      </div>

      {/* Account info */}
      {tenant && (
        <div className="card">
          <div className="card-header">
            <span className="card-title">Account Information</span>
          </div>
          <div className="agent-card-meta" style={{ padding: '0 20px 20px' }}>
            <div className="agent-meta-item">
              <span className="agent-meta-label">Email</span>
              <span className="agent-meta-value">{tenant.email}</span>
            </div>
            <div className="agent-meta-item">
              <span className="agent-meta-label">Plan</span>
              <span className="agent-meta-value" style={{ textTransform: 'uppercase' }}>{tenant.plan}</span>
            </div>
            <div className="agent-meta-item">
              <span className="agent-meta-label">Tenant ID</span>
              <span className="agent-meta-value mono" style={{ fontSize: 11 }}>{tenant.id}</span>
            </div>
          </div>
        </div>
      )}

      {/* Logout */}
      <div className="card" style={{ borderColor: 'var(--sg-danger)' }}>
        <div style={{ padding: 20, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div>
            <div style={{ fontWeight: 600, marginBottom: 4 }}>Sign Out</div>
            <div style={{ fontSize: 12, color: 'var(--sg-text-tertiary)' }}>End your current session</div>
          </div>
          <button className="btn btn-danger" onClick={onLogout}>
            <LogOut size={14} />
            Sign Out
          </button>
        </div>
      </div>
    </div>
  )
}
