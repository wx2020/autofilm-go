<template>
  <div>
    <h3 class="mb-4"><i class="bi-link-45deg me-2"></i>Alist2Strm 模块</h3>
    <ModuleConfigEditor type="alist2strm" :defaults="defaults" @changed="load" />
    <ModuleCard v-for="m in modules" :key="m.id" :module="m"
      @run="triggerRun(m)" @toggle="toggleModule(m)" />
    <div v-if="!modules.length" class="text-muted">暂无 Alist2Strm 配置</div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import ModuleCard from '../components/ModuleCard.vue'
import ModuleConfigEditor from '../components/ModuleConfigEditor.vue'

const modules = ref([])
const defaults = { id: 'alist-main', enable: true, run_on_start: false, url: 'http://127.0.0.1:5244', username: '', password: '', token: '', public_url: '', source_dir: '/', target_dir: '/media', flatten_mode: false, subtitle: true, image: false, nfo: false, mode: 'AlistURL', overwrite: false, sync_server: true, scan_mode: 'incremental', qps_limit: 10, max_workers: 50, max_downloaders: 5, wait_time: 0, cron: '0 0 */6 * * *' }

async function load() {
  try {
    const res = await fetch('/api/modules')
    const all = await res.json()
    modules.value = all.filter(m => m.type === 'alist2strm')
  } catch (e) { console.error(e) }
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
