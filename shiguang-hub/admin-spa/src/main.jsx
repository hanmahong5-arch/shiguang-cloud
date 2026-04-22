import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App.jsx'
import { ErrorBoundary } from './components/ErrorBoundary.jsx'
import { ConfirmProvider } from './hooks/useConfirm.jsx'
import './styles.css'

// 组件树外层顺序（由外到内）：
//   ErrorBoundary → BrowserRouter → ConfirmProvider → App
// ErrorBoundary 必须最外层以覆盖 Provider / Router 自身异常。
createRoot(document.getElementById('root')).render(
  <StrictMode>
    <ErrorBoundary>
      <BrowserRouter basename="/admin">
        <ConfirmProvider>
          <App />
        </ConfirmProvider>
      </BrowserRouter>
    </ErrorBoundary>
  </StrictMode>,
)
