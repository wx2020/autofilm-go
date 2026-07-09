<template>
  <div>
    <h3 class="mb-4"><i class="bi-speedometer2 me-2"></i>系统概览</h3>
    <div class="row">
      <div class="col-md-3 mb-3" v-for="stat in stats" :key="stat.label">
        <div class="card text-center">
          <div class="card-body">
            <h5 class="card-title">{{ stat.count }}</h5>
            <p class="card-text text-muted">{{ stat.label }}</p>
          </div>
        </div>
      </div>
    </div>
    <h5 class="mt-4 mb-3">模块列表</h5>
    <ModuleCard v-for="m in modules" :key="m.id" :module="m"
      @run="triggerRun(m)" @toggle="toggleModule(m)" />
    <div v-if="!modules.length" class="text-muted">暂无已配置的模块</div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import ModuleCard from '../components/ModuleCard.vue'

const modules = ref([])
const stats = ref([])

async function loadModules() {
  try {
    const res = await fetch('/api/modules')
    modules.value = await res.json()
    stats.value = [
      { label: 'Alist2Strm', count: modules.value.filter(m => m.type === 'alist2strm').length },
      { label: 'Ani2Alist', count: modules.value.filter(m => m.type === 'ani2alist').length },
      { label: 'LibraryPoster', count: modules.value.filter(m => m.type === 'libraryposter').length },
      { label: 'Alissync', count: modules.value.filter(m => m.type === 'alissync').length },
    ]
  } catch (e) {
    console.error(e)
  }
}

async function triggerRun(m) {
  await fetch(`/api/modules/${m.type}/${m.id}/run`, { method: 'POST' })
  loadModules()
}

async function toggleModule(m) {
  await fetch(`/api/modules/${m.type}/${m.id}/toggle`, { method: 'POST' })
  loadModules()
}

onMounted(loadModules)
</script>
