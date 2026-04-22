/**
 * useConfirm + ConfirmProvider
 * ------------------------------------------------------------------
 * Promise 风格的全局确认对话框：
 *
 *   const confirm = useConfirm()
 *   const ok = await confirm({ title: 'Delete Server Line?', danger: true })
 *   if (ok) performDelete()
 *
 * 与 launcher 端 `shiguang-launcher/frontend/src/components/ConfirmDialog.tsx`
 * 语义一致；样式共用 --sg-* 设计 token，保持跨项目视觉统一。
 *
 * 设计要点：
 *   - 单例挂载：同时最多展示一个
 *   - Esc = 取消 / Enter = 确认（自动聚焦 confirm 按钮）
 *   - 背景点击取消
 *   - danger 模式：确认按钮改 btn-danger
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from 'react'

const ConfirmContext = createContext(null)

export function useConfirm() {
  const fn = useContext(ConfirmContext)
  if (!fn) throw new Error('useConfirm must be used within <ConfirmProvider>')
  return fn
}

export function ConfirmProvider({ children }) {
  const [state, setState] = useState(null)
  const confirmBtnRef = useRef(null)

  const confirm = useCallback((opts) => {
    return new Promise((resolve) => {
      setState({ opts, resolver: resolve })
    })
  }, [])

  const close = useCallback((result) => {
    setState((cur) => {
      if (cur) cur.resolver(result)
      return null
    })
  }, [])

  useEffect(() => {
    if (!state) return
    const onKey = (e) => {
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

  useEffect(() => {
    if (state) {
      const id = window.setTimeout(() => confirmBtnRef.current?.focus(), 0)
      return () => window.clearTimeout(id)
    }
  }, [state])

  // Provider 销毁时 resolve(false) 防止 Promise 永挂
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
          className="sg-confirm-backdrop"
          role="dialog"
          aria-modal="true"
          aria-labelledby="sg-confirm-title"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) close(false)
          }}
        >
          <div className={`sg-confirm-dialog ${state.opts.danger ? 'sg-confirm-danger' : ''}`}>
            <h3 id="sg-confirm-title" className="sg-confirm-title">
              {state.opts.title}
            </h3>
            {state.opts.message && (
              <p className="sg-confirm-message">{state.opts.message}</p>
            )}
            <div className="sg-confirm-actions">
              <button
                className="btn btn-ghost"
                type="button"
                onClick={() => close(false)}
              >
                {state.opts.cancelLabel || 'Cancel'}
              </button>
              <button
                ref={confirmBtnRef}
                className={state.opts.danger ? 'btn btn-danger' : 'btn btn-primary'}
                type="button"
                onClick={() => close(true)}
              >
                {state.opts.confirmLabel || 'Confirm'}
              </button>
            </div>
          </div>
        </div>
      )}
    </ConfirmContext.Provider>
  )
}
