import { createRouter, createWebHistory } from 'vue-router'
import Overview from '../views/Overview.vue'
import Alist2Strm from '../views/Alist2Strm.vue'
import Ani2Alist from '../views/Ani2Alist.vue'
import LibraryPoster from '../views/LibraryPoster.vue'
import Sync from '../views/Sync.vue'
import Settings from '../views/Settings.vue'
import Logs from '../views/Logs.vue'

const routes = [
  { path: '/', name: 'Overview', component: Overview },
  { path: '/alist2strm', name: 'Alist2Strm', component: Alist2Strm },
  { path: '/ani2alist', name: 'Ani2Alist', component: Ani2Alist },
  { path: '/libraryposter', name: 'LibraryPoster', component: LibraryPoster },
  { path: '/sync', name: 'Sync', component: Sync },
  { path: '/settings', name: 'Settings', component: Settings },
  { path: '/logs', name: 'Logs', component: Logs },
]

export default createRouter({
  history: createWebHistory(),
  routes,
})
