import { createApp } from 'vue'
import 'bootstrap/dist/css/bootstrap.min.css'
import 'bootstrap-icons/font/bootstrap-icons.css'
import 'bootstrap'
import App from './App.vue'
import router from './router'

// 可通过 http://host:8080/?token=... 首次进入受保护的管理界面。
// Token 仅保存在当前标签页，并自动附加到同源 API 请求。
const queryToken = new URLSearchParams(window.location.search).get('token')
if (queryToken) {
  sessionStorage.setItem('autofilm_token', queryToken)
  window.history.replaceState({}, '', window.location.pathname)
}
const nativeFetch = window.fetch.bind(window)
window.fetch = (input, init = {}) => {
  const url = typeof input === 'string' ? input : input.url
  const token = sessionStorage.getItem('autofilm_token')
  if (token && url.startsWith('/api/')) {
    const headers = new Headers(init.headers || (typeof input === 'string' ? undefined : input.headers))
    headers.set('Authorization', `Bearer ${token}`)
    init = { ...init, headers }
  }
  return nativeFetch(input, init).then(response => {
    if (response.status === 401 && !url.startsWith('/api/auth/')) {
      sessionStorage.removeItem('autofilm_token')
      sessionStorage.removeItem('autofilm_user')
      if (location.pathname !== '/login') location.assign('/login')
    }
    return response
  })
}

createApp(App).use(router).mount('#app')
