import { render } from 'preact'
import './style.css'

// Reports JavaScript errors to the server journal (linos.log): phones and the TV have no visible console.
function report(message: string, stack?: string) {
  const body = JSON.stringify({ page: location.pathname, message, stack })
  navigator.sendBeacon?.('/api/log', body) || fetch('/api/log', { method: 'POST', body, keepalive: true }).catch(() => {})
}
window.addEventListener('error', (e) => report(e.message, e.error?.stack))
window.addEventListener('unhandledrejection', (e) => report(String(e.reason), e.reason?.stack))

// mayControl tells whether this browser can pilot: with an invite or a gamemaster token already
// stored, or with the host's rights (the host machine, or signed in), which a host-only route answers.
async function mayControl() {
  const { hostRights, store } = await import('./ws')
  return new URLSearchParams(location.search).has('invite') || !!store('linos-master') || hostRights()
}

// Each page is its own chunk: phones never download the control or host code.
const pages: Record<string, () => Promise<{ default: () => preact.JSX.Element }>> = {
  '/control': async () => ((await mayControl()) ? import('./control') : import('./access')),
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
