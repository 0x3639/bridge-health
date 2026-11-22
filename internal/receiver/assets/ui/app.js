import { createApp } from './vue.js'
import PublicStatus from './PublicStatus.js'

const root = document.getElementById('app')
if (root.dataset.view === 'public') {
  const initial = JSON.parse(document.getElementById('public-snapshot').textContent)
  createApp(PublicStatus, { initial }).mount(root)
} else {
  import('./Management.js').then(({ default: Management }) => {
    createApp(Management, { page: JSON.parse(document.getElementById('page').textContent) }).mount(
      root,
    )
  })
}
