/**
 * ErrorBoundary
 * ------------------------------------------------------------------
 * 顶层错误边界：捕获渲染阶段抛出的异常，展示恢复 UI 代替白屏。
 *
 * 设计要点：
 *   - 不依赖 toast / context，独立运转（因为 ToastProvider 本身可能崩溃）
 *   - 提供“重新加载”按钮（window.location.reload() 让 Wails 重启前端）
 *   - 折叠显示错误栈，默认隐藏，点击展开，便于玩家截图反馈
 *   - 支持 brand color fallback：无品牌时用中性灰以免被覆写变量拖垮
 */
import { Component, ErrorInfo, ReactNode } from 'react'

interface Props {
  children: ReactNode
}

interface State {
  hasError: boolean
  error: Error | null
  errorInfo: ErrorInfo | null
  showDetails: boolean
}

export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false, error: null, errorInfo: null, showDetails: false }
  }

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    // 生产环境下，错误会被 Wails 日志（Go 侧 runtime.LogError）捕获，
    // 此处仅打印到控制台供开发调试。
    console.error('[ErrorBoundary] caught:', error, errorInfo)
    this.setState({ errorInfo })
  }

  handleReload = () => {
    // Wails 前端热重载：reload 会触发 Go 侧重新加载 index.html
    window.location.reload()
  }

  toggleDetails = () => {
    this.setState((s) => ({ showDetails: !s.showDetails }))
  }

  render() {
    if (!this.state.hasError) return this.props.children

    const { error, errorInfo, showDetails } = this.state

    return (
      <div className="error-boundary">
        <div className="error-boundary-card">
          <div className="error-boundary-icon" aria-hidden="true">!</div>
          <h2 className="error-boundary-title">启动器遇到了问题</h2>
          <p className="error-boundary-desc">
            页面渲染时出现未处理的异常。点击“重新加载”通常可恢复；
            若反复出现，请截图下方错误信息并联系服务器管理员。
          </p>

          <div className="error-boundary-actions">
            <button className="btn-primary" onClick={this.handleReload}>
              重新加载
            </button>
            <button className="btn-secondary" onClick={this.toggleDetails}>
              {showDetails ? '隐藏详情' : '显示详情'}
            </button>
          </div>

          {showDetails && (
            <pre className="error-boundary-details">
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
