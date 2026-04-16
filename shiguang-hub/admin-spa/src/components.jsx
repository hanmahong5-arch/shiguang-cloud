import { useState, useEffect, useCallback, Component } from 'react'
import { CheckCircle, XCircle, Info, X, Copy, Check, WifiOff, RefreshCw, AlertTriangle } from 'lucide-react'
import { useToast } from './hooks/useToast'
import { onNetworkChange, isOnline } from './api'

// --- Toast Renderer (placed in App shell) ---
export function ToastContainer() {
  const { toasts, dismiss } = useToast()
  const icons = {
    success: <CheckCircle />,
    error: <XCircle />,
    info: <Info />,
  }

  return (
    <div className="toast-container">
      {toasts.map(t => (
        <div key={t.id} className={`toast toast-${t.type}${t.exiting ? ' exiting' : ''}`}>
          {icons[t.type]}
          <span className="toast-msg">{t.message}</span>
          <button className="toast-close" onClick={() => dismiss(t.id)}>
            <X size={14} />
          </button>
        </div>
      ))}
    </div>
  )
}

// --- Skeleton Loader ---
export function Skeleton({ variant = 'text', width, height, count = 1, className = '' }) {
  const items = Array.from({ length: count })
  return items.map((_, i) => (
    <div
      key={i}
      className={`skeleton skeleton-${variant} ${className}`}
      style={{ width, height }}
    />
  ))
}

// --- Empty State ---
export function EmptyState({ icon, title, description, action }) {
  return (
    <div className="empty-state">
      {icon && <div className="empty-state-icon">{icon}</div>}
      <div className="empty-state-title">{title}</div>
      {description && <div className="empty-state-desc">{description}</div>}
      {action}
    </div>
  )
}

// --- Copy Button ---
export function CopyButton({ text, label = 'Copy' }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Fallback for non-HTTPS contexts
      const ta = document.createElement('textarea')
      ta.value = text
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      document.body.removeChild(ta)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }, [text])

  return (
    <button className={`copy-btn${copied ? ' copied' : ''}`} onClick={handleCopy} title={label}>
      {copied ? <Check size={14} /> : <Copy size={14} />}
      {copied ? 'Copied' : label}
    </button>
  )
}

// --- Confirm Modal ---
export function ConfirmModal({ title, message, confirmLabel = 'Confirm', onConfirm, onCancel, danger = false }) {
  return (
    <div className="modal-overlay" onClick={onCancel}>
      <div className="modal-card" onClick={e => e.stopPropagation()}>
        <div className="modal-title">{title}</div>
        <p style={{ fontSize: 13, color: 'var(--sg-text-secondary)', lineHeight: 1.6 }}>{message}</p>
        <div className="modal-actions">
          <button className="btn btn-secondary" onClick={onCancel}>Cancel</button>
          <button className={`btn ${danger ? 'btn-danger' : 'btn-primary'}`} onClick={onConfirm}>
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}

// --- Error Boundary ---
// Catches render errors in child components and shows a recovery UI
// instead of a white screen. Users can retry without refreshing.
export class ErrorBoundary extends Component {
  constructor(props) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error) {
    return { hasError: true, error }
  }

  componentDidCatch(error, info) {
    console.error('[ErrorBoundary]', error, info.componentStack)
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="error-boundary">
          <AlertTriangle size={32} />
          <div className="error-boundary-title">Something went wrong</div>
          <div className="error-boundary-msg">
            {this.state.error?.message || 'An unexpected error occurred'}
          </div>
          <button
            className="btn btn-primary"
            onClick={() => this.setState({ hasError: false, error: null })}
          >
            <RefreshCw size={14} /> Try Again
          </button>
        </div>
      )
    }
    return this.props.children
  }
}

// --- Network Status Banner ---
// Shows a non-intrusive banner when the browser goes offline.
export function NetworkBanner() {
  const [online, setOnline] = useState(isOnline())

  useEffect(() => onNetworkChange(setOnline), [])

  if (online) return null
  return (
    <div className="network-banner">
      <WifiOff size={14} />
      <span>You are offline. Changes will not be saved until connection is restored.</span>
    </div>
  )
}

// --- Relative Time Formatter ---
export function timeAgo(date) {
  if (!date) return '\u2014'
  const d = date instanceof Date ? date : new Date(date)
  const s = Math.floor((Date.now() - d.getTime()) / 1000)
  if (s < 5) return 'just now'
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

// --- Format Date ---
export function formatDate(date) {
  if (!date) return '\u2014'
  return new Date(date).toLocaleDateString('zh-CN', {
    year: 'numeric', month: 'short', day: 'numeric',
  })
}
