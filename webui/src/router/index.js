import { createRouter, createWebHistory } from 'vue-router'
import Overview from '../views/Overview.vue'
import Alist2Strm from '../views/Alist2Strm.vue'
import Ani2Alist from '../views/Ani2Alist.vue'
import LibraryPoster from '../views/LibraryPoster.vue'
import Sync from '../views/Sync.vue'
import FileMove from '../views/FileMove.vue'
import Settings from '../views/Settings.vue'
import Logs from '../views/Logs.vue'
import Login from '../views/Login.vue'
import Admin from '../views/Admin.vue'
import Monitoring from '../views/Monitoring.vue'

const routes = [
  { path: '/', name: 'Overview', component: Overview },
  { path: '/alist2strm', name: 'Alist2Strm', component: Alist2Strm },
  { path: '/ani2alist', name: 'Ani2Alist', component: Ani2Alist },
  { path: '/libraryposter', name: 'LibraryPoster', component: LibraryPoster },
  { path: '/sync', name: 'Sync', component: Sync },
  { path: '/filemove', name: 'FileMove', component: FileMove },
  { path: '/settings', name: 'Settings', component: Settings },
  { path: '/logs', name: 'Logs', component: Logs },
  { path: '/monitoring', name: 'Monitoring', component: Monitoring },
  { path: '/admin', name: 'Admin', component: Admin },
  { path: '/login', name: 'Login', component: Login, meta: { public: true } },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach(async (to) => {
  if (to.meta.public) return true
  const status = await fetch('/api/auth/status').then(r => r.json()).catch(() => ({ initialized: false }))
  const token = sessionStorage.getItem('autofilm_token')
  if (!status.initialized && !(status.legacy_token && token)) return '/login'
  if (!token) return '/login'
  return true
})

export default router
