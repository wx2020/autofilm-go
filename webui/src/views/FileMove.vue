<template>
  <div>
    <h3 class="mb-4"><i class="bi-folder-symlink me-2"></i>递归移动文件</h3>
    <div class="alert alert-info py-2">
      按相对路径正则和文件大小筛选文件，定时从源目录移动到目标目录，并保留原有目录结构。
    </div>
    <ModuleConfigEditor type="filemove" :defaults="defaults" @changed="load" />
    <ModuleCard v-for="m in modules" :key="m.id" :module="m"
      @run="triggerRun(m)" @toggle="toggleModule(m)" />
    <div v-if="!modules.length" class="text-muted">暂无递归移动文件配置</div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import ModuleCard from '../components/ModuleCard.vue'
import ModuleConfigEditor from '../components/ModuleConfigEditor.vue'

const modules = ref([])
const defaults = {
  id: 'file-archive', enable: true, run_on_start: false, backend: 'local', url: 'http://127.0.0.1:5244', username: '', password: '', token: '',
  source_dir: 'D:/downloads', target_dir: 'D:/media', regex: '(?i)\\.(mkv|mp4)$',
  size: null, min_size: 0, max_size: 0, flatten: false, overwrite: false,
  cron: '0 0 */1 * * *'
}

async function load() {
  try {
    const res = await fetch('/api/modules')
    const all = await res.json()
    modules.value = Array.isArray(all) ? all.filter(m => m.type === 'filemove') : []
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
