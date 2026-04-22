/**
 * ErrorBoundary
 * ------------------------------------------------------------------
 * 顶层错误边界：捕获渲染阶段的 JS 异常，展示恢复 UI 代替白屏。
 *
 * 与 launcher 端 ErrorBoundary.tsx 语义一致，样式共用 --sg-* 设计 token。
 */
import { Component } from 'react'

export class ErrorBoundary extends Component {
  constructor(props) {
    super(props)
    this.state = { hasError: false, error: null, errorInfo: null, showDetails: false }
  }

  static getDerivedStateFromError(error) {
    return { hasError: true, error }
  }

  componentDidCatch(error, errorInfo) {
    // eslint-disable-next-line no-console
    console.error('[ErrorBoundary] caught:', error, errorInfo)
    this.setState({ errorInfo })
  }

  handleReload = () => {
    window.location.reload()
  }

  toggleDetails = () => {
    this.setState((s) => ({ showDetails: !s.showDetails }))
  }

  render() {
    if (!this.state.hasError) return this.props.children

    const { error, errorInfo, showDetails } = this.state

    return (
      <div className="sg-error-boundary">
        <div className="sg-error-boundary-card">
          <div className="sg-error-boundary-icon" aria-hidden="true">!</div>
          <h2 className="sg-error-boundary-title">Dashboard Error</h2>
          <p className="sg-error-boundary-desc">
            An unexpected error occurred while rendering the dashboard.
            Reloading usually recovers; if this keeps happening, please copy the error details
            below and contact support.
          </p>

          <div className="sg-error-boundary-actions">
            <button className="btn btn-primary" onClick={this.handleReload}>
              Reload
            </button>
            <button className="btn btn-ghost" onClick={this.toggleDetails}>
              {showDetails ? 'Hide details' : 'Show details'}
            </button>
          </div>

          {showDetails && (
            <pre className="sg-error-boundary-details">
              {error?.name}: {error?.message}
              {'\n\n'}
              {error?.stack || '(no stack trace)'}
              {errorInfo?.componentStack ? '\n\n' + errorInfo.componentStack : ''}
            </pre>
          )}
        </div>
      </div>
    )
  }
}
