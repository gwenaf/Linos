import { render } from 'preact'
import './style.css'

// Reports JavaScript errors to the server journal (linos.log): phones and the TV have no visible console.
function report(message: string, stack?: string) {
  const body = JSON.stringify({ page: location.pathname, message, stack })
  navigator.sendBeacon?.('/api/log', body) || fetch('/api/log', { method: 'POST', body, keepalive: true }).catch(() => {})
}
window.addEventListener('error', (e) => report(e.message, e.error?.stack))
window.addEventListener('unhandledrejection', (e) => report(String(e.reason), e.reason?.stack))

// Each page is its own chunk: phones never download the control or host code.
const pages: Record<string, () => Promise<{ default: () => preact.JSX.Element }>> = {
  '/control': () => import('./control'),
  '/host': () => import('./host'),
  '/play': () => import('./play'),
  '/edit': () => import('./edit'),
}

const load = pages[location.pathname] ?? pages['/play']
load()
  .then(({ default: Page }) => render(<Page />, document.getElementById('app')!))
  .catch((err) => {
    report(`page failed to load: ${err}`, err?.stack)
    document.getElementById('app')!.textContent = 'Linos : la page ne se charge pas. Rechargez-la.'
  })
