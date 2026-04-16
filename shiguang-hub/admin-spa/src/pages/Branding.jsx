import { useState, useEffect } from 'react'
import { Save, AlertCircle, Eye } from 'lucide-react'
import * as api from '../api'
import { Skeleton } from '../components'
import { useToast } from '../hooks/useToast'

// Sanitize URLs to prevent javascript: XSS injection
function safeUrl(url) {
  if (!url) return ''
  try {
    const parsed = new URL(url)
    if (parsed.protocol === 'http:' || parsed.protocol === 'https:') return url
  } catch {}
  return '' // reject non-http(s) URLs
}

const DEFAULT_THEME = {
  server_name: '', logo_url: '', bg_url: '',
  accent_color: '#4f8ef7', text_color: '#e6e8eb',
  news_url: '', patch_url: '',
}

export default function BrandingPage() {
  const [theme, setTheme] = useState(DEFAULT_THEME)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const toast = useToast()

  useEffect(() => {
    api.getTheme()
      .then(t => { if (t) setTheme(prev => ({ ...prev, ...t })) })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  const save = async () => {
    setError('')
    setSaving(true)
    try {
      await api.updateTheme(theme)
      toast.success('Branding saved successfully')
    } catch (err) {
      setError(err.message)
      toast.error('Failed to save branding')
    } finally {
      setSaving(false)
    }
  }

  const set = (k, v) => setTheme(prev => ({ ...prev, [k]: v }))

  if (loading) {
    return (
      <div className="page-enter">
        <div className="page-header">
          <div>
            <Skeleton variant="title" />
            <Skeleton variant="text" width="200px" />
          </div>
        </div>
        <div className="card"><Skeleton variant="row" count={5} /></div>
      </div>
    )
  }

  return (
    <div className="page-enter">
      <div className="page-header">
        <div>
          <div className="page-title">Launcher Branding</div>
          <div className="page-subtitle">Customize how your server appears in the player launcher</div>
        </div>
        <button className="btn btn-primary" onClick={save} disabled={saving}>
          <Save size={16} />
          {saving ? 'Saving...' : 'Save Changes'}
        </button>
      </div>

      {error && <div className="error-msg"><AlertCircle /><span>{error}</span></div>}

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, alignItems: 'start' }}>
        {/* Form side */}
        <div className="card">
          <div className="card-header">
            <span className="card-title">Theme Settings</span>
          </div>

          <div className="form-field">
            <label className="form-label">Server Name</label>
            <input
              value={theme.server_name}
              onChange={e => set('server_name', e.target.value)}
              placeholder="Your Server Name"
            />
            <span className="form-hint">Displayed in the launcher title bar</span>
          </div>

          <div className="form-field">
            <label className="form-label">Logo URL</label>
            <input
              value={theme.logo_url}
              onChange={e => set('logo_url', e.target.value)}
              placeholder="https://cdn.example.com/logo.png"
            />
          </div>

          <div className="form-field">
            <label className="form-label">Background Image URL</label>
            <input
              value={theme.bg_url}
              onChange={e => set('bg_url', e.target.value)}
              placeholder="https://cdn.example.com/bg.jpg"
            />
          </div>

          <div className="form-grid">
            <div className="form-field">
              <label className="form-label">Accent Color</label>
              <div className="color-input-row">
                <div className="color-swatch" style={{ background: theme.accent_color }}>
                  <input
                    type="color"
                    value={theme.accent_color}
                    onChange={e => set('accent_color', e.target.value)}
                  />
                </div>
                <input
                  value={theme.accent_color}
                  onChange={e => set('accent_color', e.target.value)}
                  placeholder="#4f8ef7"
                  style={{ flex: 1 }}
                />
              </div>
            </div>
            <div className="form-field">
              <label className="form-label">Text Color</label>
              <div className="color-input-row">
                <div className="color-swatch" style={{ background: theme.text_color }}>
                  <input
                    type="color"
                    value={theme.text_color}
                    onChange={e => set('text_color', e.target.value)}
                  />
                </div>
                <input
                  value={theme.text_color}
                  onChange={e => set('text_color', e.target.value)}
                  placeholder="#e6e8eb"
                  style={{ flex: 1 }}
                />
              </div>
            </div>
          </div>

          <div className="form-field">
            <label className="form-label">News URL</label>
            <input
              value={theme.news_url}
              onChange={e => set('news_url', e.target.value)}
              placeholder="https://news.example.com"
            />
            <span className="form-hint">Embedded in the launcher news panel (iframe)</span>
          </div>

          <div className="form-field">
            <label className="form-label">Patch Manifest URL</label>
            <input
              value={theme.patch_url}
              onChange={e => set('patch_url', e.target.value)}
              placeholder="https://patch.example.com/manifest.json"
            />
            <span className="form-hint">Players download game updates from this endpoint</span>
          </div>
        </div>

        {/* Preview side */}
        <div className="card">
          <div className="card-header">
            <span className="card-title"><Eye size={14} style={{ verticalAlign: -2 }} /> Launcher Preview</span>
          </div>
          <div
            className="preview-frame"
            style={{
              background: safeUrl(theme.bg_url)
                ? `url(${safeUrl(theme.bg_url)}) center/cover no-repeat`
                : `linear-gradient(135deg, ${theme.accent_color}22, #0a0e16)`,
            }}
          >
            <div className="preview-frame-inner">
              <span className="preview-frame-label">Preview</span>
              {safeUrl(theme.logo_url) && (
                <img
                  className="preview-logo"
                  src={safeUrl(theme.logo_url)}
                  alt="logo"
                  onError={e => { e.target.style.display = 'none' }}
                />
              )}
              <div className="preview-title" style={{ color: theme.text_color }}>
                {theme.server_name || 'Server Name'}
              </div>
              <div style={{
                padding: '8px 24px',
                borderRadius: 6,
                background: `linear-gradient(135deg, ${theme.accent_color}, ${theme.accent_color}88)`,
                color: '#fff',
                fontSize: 13,
                fontWeight: 500,
              }}>
                Launch Game
              </div>
            </div>
          </div>

          {/* Thumbnail previews of loaded images */}
          {(theme.logo_url || theme.bg_url) && (
            <div style={{ marginTop: 14, display: 'flex', gap: 10 }}>
              {theme.logo_url && (
                <div style={{ flex: 1 }}>
                  <div className="form-label" style={{ marginBottom: 4 }}>Logo</div>
                  <img
                    src={theme.logo_url}
                    alt="logo preview"
                    style={{
                      maxHeight: 40, maxWidth: '100%',
                      borderRadius: 4, objectFit: 'contain',
                      background: 'rgba(255,255,255,0.04)', padding: 4,
                    }}
                    onError={e => { e.target.style.display = 'none' }}
                  />
                </div>
              )}
              {theme.bg_url && (
                <div style={{ flex: 1 }}>
                  <div className="form-label" style={{ marginBottom: 4 }}>Background</div>
                  <img
                    src={theme.bg_url}
                    alt="bg preview"
                    style={{
                      maxHeight: 40, maxWidth: '100%',
                      borderRadius: 4, objectFit: 'cover',
                    }}
                    onError={e => { e.target.style.display = 'none' }}
                  />
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
