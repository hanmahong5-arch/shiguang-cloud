import { useState } from 'react'
import { Shield, AlertCircle } from 'lucide-react'

export default function LoginPage({ onLogin }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const submit = async (e) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      await onLogin(email, password)
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-page">
      <div className="app-bg" />
      <form className="login-card" onSubmit={submit}>
        <div className="login-brand">
          <Shield size={40} strokeWidth={1.5} />
          <h1>SHIGUANG CLOUD</h1>
          <p>Operator Management Console</p>
        </div>

        {error && (
          <div className="error-msg">
            <AlertCircle />
            <span>{error}</span>
          </div>
        )}

        <div className="form-field">
          <label className="form-label">Email</label>
          <input
            type="email"
            value={email}
            onChange={e => setEmail(e.target.value)}
            placeholder="admin@example.com"
            required
            autoFocus
            autoComplete="email"
          />
        </div>

        <div className="form-field">
          <label className="form-label">Password</label>
          <input
            type="password"
            value={password}
            onChange={e => setPassword(e.target.value)}
            placeholder="Enter your password"
            required
            autoComplete="current-password"
          />
        </div>

        <button className="btn btn-primary" type="submit" disabled={loading}>
          {loading ? 'Authenticating...' : 'Sign In'}
        </button>
      </form>
    </div>
  )
}
