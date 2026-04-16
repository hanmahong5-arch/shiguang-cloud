/**
 * ConfirmDialog + ConfirmProvider
 * ------------------------------------------------------------------
 * Promise 风格的全局确认对话框：
 *
 *   const confirm = useConfirm()
 *   const ok = await confirm({ title: '退出登录?', danger: true })
 *   if (ok) doLogout()
 *
 * 设计要点：
 *   - 单例挂载：同时最多展示一个，后入的请求排队等待
 *   - 键盘：Esc = 取消，Enter = 确认（自动聚焦“确认”按钮）
 *   - 背景点击取消（避免意外穿透）
 *   - danger 模式：确认按钮改用红色，突出破坏性语义
 *   - 组件卸载时 resolve(false)，防止 Promise 永挂
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  ReactNode,
} from 'react'

export interface ConfirmOptions {
  /** 对话框标题（必填） */
  title: string
  /** 对话框正文，可选；支持换行 */
  message?: string
  /** 确认按钮文案，默认“确认” */
  confirmLabel?: string
  /** 取消按钮文案，默认“取消” */
  cancelLabel?: string
  /** 破坏性操作，确认按钮使用红色 */
  danger?: boolean
}

type Resolver = (ok: boolean) => void

const ConfirmContext = createContext<((opts: ConfirmOptions) => Promise<boolean>) | null>(null)

/**
 * useConfirm —— 在组件内拿到 confirm(opts) 函数。
 * 必须在 <ConfirmProvider> 下调用。
 */
export function useConfirm() {
  const fn = useContext(ConfirmContext)
  if (!fn) throw new Error('useConfirm must be used within <ConfirmProvider>')
  return fn
}

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<{ opts: ConfirmOptions; resolver: Resolver } | null>(null)
  const confirmBtnRef = useRef<HTMLButtonElement>(null)

  const confirm = useCallback((opts: ConfirmOptions) => {
    return new Promise<boolean>((resolve) => {
      setState({ opts, resolver: resolve })
    })
  }, [])

  const close = useCallback((result: boolean) => {
    setState((cur) => {
      if (cur) cur.resolver(result)
      return null
    })
  }, [])

  // 键盘处理：Esc 取消 / Enter 确认
  useEffect(() => {
    if (!state) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        close(false)
      } else if (e.key === 'Enter') {
        e.preventDefault()
        close(true)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [state, close])

  // 打开时聚焦确认按钮；关闭时无需处理（上一个焦点由浏览器自动恢复）
  useEffect(() => {
    if (state) {
      // 推到下一 tick，等 DOM 挂载完毕
      const id = window.setTimeout(() => confirmBtnRef.current?.focus(), 0)
      return () => window.clearTimeout(id)
    }
  }, [state])

  // 组件卸载时收尾：Provider 被销毁则视作全部 Promise 都取消
  useEffect(() => {
    return () => {
      if (state) state.resolver(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {state && (
        <div
          className="confirm-backdrop"
          role="dialog"
          aria-modal="true"
          aria-labelledby="confirm-title"
          onMouseDown={(e) => {
            // 仅当点击的是 backdrop 本身（而不是冒泡自 dialog）才关闭
            if (e.target === e.currentTarget) close(false)
          }}
        >
          <div className={`confirm-dialog ${state.opts.danger ? 'confirm-danger' : ''}`}>
            <h3 id="confirm-title" className="confirm-title">
              {state.opts.title}
            </h3>
            {state.opts.message && (
              <p className="confirm-message">{state.opts.message}</p>
            )}
            <div className="confirm-actions">
              <button
                className="btn-secondary"
                type="button"
                onClick={() => close(false)}
              >
                {state.opts.cancelLabel || '取消'}
              </button>
              <button
                ref={confirmBtnRef}
                className={state.opts.danger ? 'btn-danger' : 'btn-primary confirm-btn-primary'}
                type="button"
                onClick={() => close(true)}
              >
                {state.opts.confirmLabel || '确认'}
              </button>
            </div>
          </div>
        </div>
      )}
    </ConfirmContext.Provider>
  )
}
