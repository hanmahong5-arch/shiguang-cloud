import { useState, useEffect, useRef } from 'react'
import { Radio, RefreshCw, ExternalLink, RotateCw } from 'lucide-react'
import * as api from '../api'
import { Skeleton, EmptyState, timeAgo, ConfirmModal, CopyButton } from '../components'
import { useToast } from '../hooks/useToast'

const REFRESH_INTERVAL = 15000 // 15 seconds

export default function AgentsPage() {
  const [agents, setAgents] = useState(null)
  const [refreshing, setRefreshing] = useState(false)
  const [countdown, setCountdown] = useState(REFRESH_INTERVAL / 1000)
  const [rotateTarget, setRotateTarget] = useState(null) // agent to rotate key for
  const [newKey, setNewKey] = useState(null) // newly rotated key to display
  const intervalRef = useRef(null)
  const toast = useToast()

  const load = async () => {
    setRefreshing(true)
    try {
      const data = await api.getAgents()
      setAgents(data)
    } catch {
      setAgents(prev => prev ?? [])
    }
    setRefreshing(false)
    setCountdown(REFRESH_INTERVAL / 1000)
  }

  useEffect(() => { load() }, [])

  // Auto-refresh timer
  useEffect(() => {
    intervalRef.current = setInterval(load, REFRESH_INTERVAL)
    return () => clearInterval(intervalRef.current)
  }, [])

  // Countdown display
  useEffect(() => {
    const tick = setInterval(() => {
      setCountdown(prev => (prev <= 1 ? REFRESH_INTERVAL / 1000 : prev - 1))
    }, 1000)
    return () => clearInterval(tick)
  }, [])

  const manualRefresh = () => {
    clearInterval(intervalRef.current)
    load()
    intervalRef.current = setInterval(load, REFRESH_INTERVAL)
  }

  const handleRotateKey = async () => {
    if (!rotateTarget) return
    try {
      const res = await api.rotateAgentKey(rotateTarget.id)
      setNewKey(res.new_key)
      setRotateTarget(null)
      toast.success('Agent key rotated. Copy the new key — it will not be shown again.')
    } catch (err) {
      toast.error('Rotate failed: ' + err.message)
    }
  }

  return (
    <div className="page-enter">
      <div className="page-header">
        <div>
          <div className="page-title">Gate Agents</div>
          <div className="page-subtitle">Monitor your on-premise gate agent deployment</div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <div className={`refresh-indicator${refreshing ? ' refreshing' : ''}`}>
            <RefreshCw size={14} />
            <span>Refresh in {countdown}s</span>
          </div>
          <button className="btn btn-secondary" onClick={manualRefresh} disabled={refreshing}>
            <RefreshCw size={16} />
            Refresh
          </button>
        </div>
      </div>

      {agents === null ? (
        <div className="agent-grid">
          {[1, 2].map(i => (
            <div key={i} className="card">
              <Skeleton variant="row" count={3} />
            </div>
          ))}
        </div>
      ) : agents.length === 0 ? (
        <div className="card">
          <EmptyState
            icon={<Radio />}
            title="No agents registered"
            description="Deploy shiguang-agent on your game server with the agent token from your account setup. Once started, it will appear here automatically."
          />
        </div>
      ) : (
        <div className="agent-grid">
          {agents.map(a => {
            const ms = Date.now() - new Date(a.last_seen).getTime()
            const stale = ms > 60000
            const status = stale ? 'stale' : a.status === 'online' ? 'online' : 'offline'

            return (
              <div key={a.id} className="agent-card">
                <div className="agent-card-header">
                  <span className={`status-dot ${status}`} />
                  <span className="agent-card-name">{a.hostname}</span>
                  <span style={{ marginLeft: 'auto' }}>
                    <span className={`badge badge-${status === 'online' ? 'online' : status === 'stale' ? 'offline' : 'neutral'}`}>
                      {status === 'online' ? 'Online' : status === 'stale' ? 'Unreachable' : 'Offline'}
                    </span>
                  </span>
                </div>

                <div className="agent-card-meta">
                  <div className="agent-meta-item">
                    <span className="agent-meta-label">Public IP</span>
                    <span className="agent-meta-value mono">{a.public_ip || '\u2014'}</span>
                  </div>
                  <div className="agent-meta-item">
                    <span className="agent-meta-label">Version</span>
                    <span className="agent-meta-value">{a.version}</span>
                  </div>
                  <div className="agent-meta-item">
                    <span className="agent-meta-label">Last Seen</span>
                    <span className="agent-meta-value" style={{ color: stale ? 'var(--sg-danger)' : undefined }}>
                      {timeAgo(a.last_seen)}
                    </span>
                  </div>
                  <div className="agent-meta-item">
                    <span className="agent-meta-label">Admin Port</span>
                    <span className="agent-meta-value">
                      {a.admin_port ? (
                        <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                          {a.admin_port}
                          {a.public_ip && (
                            <a
                              href={`http://${a.public_ip}:${a.admin_port}`}
                              target="_blank"
                              rel="noopener noreferrer"
                              title="Open admin panel"
                              style={{ color: 'var(--sg-primary)', display: 'inline-flex' }}
                            >
                              <ExternalLink size={12} />
                            </a>
                          )}
                        </span>
                      ) : '\u2014'}
                    </span>
                  </div>
                </div>

                <div style={{ borderTop: '1px solid var(--sg-border)', padding: '10px 16px', display: 'flex', justifyContent: 'flex-end' }}>
                  <button
                    className="btn btn-ghost"
                    style={{ fontSize: 11, color: 'var(--sg-warning)' }}
                    onClick={() => setRotateTarget(a)}
                    title="Generate a new agent key (invalidates current key)"
                  >
                    <RotateCw size={12} />
                    Rotate Key
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      )}

      {/* Rotate key confirmation */}
      {rotateTarget && (
        <ConfirmModal
          title="Rotate Agent Key"
          message={`This will generate a new key for "${rotateTarget.hostname}" and immediately invalidate the current key. The agent will disconnect until reconfigured with the new key.`}
          confirmLabel="Rotate Key"
          onConfirm={handleRotateKey}
          onCancel={() => setRotateTarget(null)}
          danger
        />
      )}

      {/* New key display */}
      {newKey && (
        <div className="modal-overlay" onClick={() => setNewKey(null)}>
          <div className="modal" onClick={e => e.stopPropagation()} style={{ maxWidth: 480 }}>
            <div className="modal-title">New Agent Key</div>
            <p style={{ color: 'var(--sg-text-secondary)', fontSize: 13, margin: '8px 0 16px' }}>
              Copy this key now. It will not be shown again.
            </p>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, background: 'var(--sg-bg-secondary)', borderRadius: 6, padding: '10px 14px' }}>
              <code className="mono" style={{ flex: 1, fontSize: 12, wordBreak: 'break-all', color: 'var(--sg-success)' }}>{newKey}</code>
              <CopyButton text={newKey} />
            </div>
            <div className="modal-actions" style={{ marginTop: 16 }}>
              <button className="btn btn-primary" onClick={() => setNewKey(null)}>Done</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
