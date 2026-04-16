import React from 'react'
import { createRoot } from 'react-dom/client'
import './style.css'
import App from './App'
import { ErrorBoundary } from './components/ErrorBoundary'
import { ToastProvider } from './components/Toast'
import { ConfirmProvider } from './components/ConfirmDialog'

const container = document.getElementById('root')
const root = createRoot(container!)

// 组件树外层顺序（由外到内）：
//   ErrorBoundary → ToastProvider → ConfirmProvider → App
//
// - ErrorBoundary 必须最外层：否则 Provider 自身渲染异常无法捕获
// - ToastProvider 在 ConfirmProvider 之外：确认对话框关闭后仍可派发 toast
// - ConfirmProvider 直接包 App：App 内所有子组件都可通过 useConfirm 访问
root.render(
  <React.StrictMode>
    <ErrorBoundary>
      <ToastProvider>
        <ConfirmProvider>
          <App />
        </ConfirmProvider>
      </ToastProvider>
    </ErrorBoundary>
  </React.StrictMode>
)
