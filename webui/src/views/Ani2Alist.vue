<template>
  <div>
    <h3 class="mb-4"><i class="bi-film me-2"></i>Ani2Alist 模块</h3>
    <ModuleCard v-for="m in modules" :key="m.id" :module="m"
      @run="triggerRun(m)" @toggle="toggleModule(m)" />
    <div v-if="!modules.length" class="text-muted">暂无 Ani2Alist 配置</div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import ModuleCard from '../components/ModuleCard.vue'

const modules = ref([])

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
