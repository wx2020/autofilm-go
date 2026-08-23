<template>
  <div>
    <h3 class="mb-4"><i class="bi-film me-2"></i>Ani2Alist 模块</h3>
    <div class="alert alert-info py-2">
      根据 Ani RSS 更新获取动画信息，并将匹配内容整理到 Alist 目录。
    </div>
    <ModuleConfigEditor type="ani2alist" :defaults="defaults" @changed="load" />
    <ModuleCard v-for="m in modules" :key="m.id" :module="m"
      @run="triggerRun(m)" @toggle="toggleModule(m)" />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import ModuleCard from '../components/ModuleCard.vue'
import ModuleConfigEditor from '../components/ModuleConfigEditor.vue'

const modules = ref([])
const defaults = { id: 'anime', enable: true, run_on_start: false, url: 'http://127.0.0.1:5244', username: '', password: '', token: '', target_dir: '/Anime', rss_update: true, src_domain: 'aniopen.an-i.workers.dev', rss_domain: 'api.ani.rip', key_word: '', cron: '0 0 */12 * * *' }

async function load() {
  const res = await fetch('/api/modules')
  const all = await res.json()
  modules.value = all.filter(m => m.type === 'ani2alist')
}

async function triggerRun(m) {
  await fetch(`/api/modules/${m.type}/${m.id}/run`, { method: 'POST' })
}

async function toggleModule(m) {
  await fetch(`/api/modules/${m.type}/${m.id}/toggle`, { method: 'POST' })
  load()
}

onMounted(load)
</script>
