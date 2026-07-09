<template>
  <div class="d-flex">
    <nav class="d-flex flex-column flex-shrink-0 p-3 bg-dark text-white vh-100" style="width: 220px">
      <a href="/" class="d-flex align-items-center mb-3 text-white text-decoration-none">
        <i class="bi-film me-2 fs-4"></i>
        <span class="fs-5">AutoFilm</span>
      </a>
      <hr>
      <ul class="nav nav-pills flex-column mb-auto">
        <li class="nav-item" v-for="item in navItems" :key="item.path">
          <router-link :to="item.path" class="nav-link text-white" active-class="active" exact-active-class="active">
            <i :class="item.icon" class="me-2"></i>
            {{ item.label }}
          </router-link>
        </li>
      </ul>
      <hr>
      <div class="text-white-50 small">
        v{{ version }}
      </div>
    </nav>
    <main class="flex-grow-1 p-4 bg-light" style="overflow-y: auto; height: 100vh">
      <router-view />
    </main>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const version = ref('dev')

fetch('/api/health').then(r => r.json()).then(d => {
  if (d.version) version.value = d.version
}).catch(() => {})

const navItems = [
  { path: '/', label: '概览', icon: 'bi-speedometer2' },
  { path: '/alist2strm', label: 'Alist2Strm', icon: 'bi-link-45deg' },
  { path: '/ani2alist', label: 'Ani2Alist', icon: 'bi-film' },
  { path: '/libraryposter', label: '封面生成', icon: 'bi-image' },
  { path: '/sync', label: '同步任务', icon: 'bi-arrow-left-right' },
  { path: '/settings', label: '系统设置', icon: 'bi-gear' },
  { path: '/logs', label: '日志', icon: 'bi-journal-text' },
]
</script>
