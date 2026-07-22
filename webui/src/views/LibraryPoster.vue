<template>
  <div>
    <h3 class="mb-4"><i class="bi-image me-2"></i>封面生成模块</h3>
    <div class="alert alert-info py-2">
      当前版本会读取 Jellyfin/Emby 媒体项目，并使用获取到的第一张海报更新媒体库封面；标题、字体和多图拼接尚未实现。
    </div>
    <ModuleConfigEditor type="libraryposter" :defaults="defaults" @changed="load" />
    <ModuleCard v-for="m in modules" :key="m.id" :module="m"
      @run="triggerRun(m)" @toggle="toggleModule(m)" />
    <div v-if="!modules.length" class="text-muted">暂无封面生成配置</div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import ModuleCard from '../components/ModuleCard.vue'
import ModuleConfigEditor from '../components/ModuleConfigEditor.vue'

const modules = ref([])
const defaults = { id: 'poster', enable: true, run_on_start: false, url: 'http://127.0.0.1:8096', api_key: '', title_font_path: '/fonts/title.ttf', subtitle_font_path: '/fonts/subtitle.ttf', configs: [{ library_name: 'Movies', title: '电影', subtitle: 'Movie Library', limit: 15 }], cron: '0 0 4 * * *' }

async function load() {
  const res = await fetch('/api/modules')
  const all = await res.json()
  modules.value = all.filter(m => m.type === 'libraryposter')
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
