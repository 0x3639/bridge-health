import { createApp } from './vue.js'
import PublicStatus from './PublicStatus.js'

const root = document.getElementById('app')
if (root.dataset.view === 'public') {
  createApp(PublicStatus).mount(root)
} else {
  import('./Management.js').then(({ default: Management }) => {
    createApp(Management, { page: JSON.parse(document.getElementById('page').textContent) }).mount(
      root,
    )
  })
}
