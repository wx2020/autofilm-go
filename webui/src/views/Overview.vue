<template>
  <div>
    <h3 class="mb-4"><i class="bi-speedometer2 me-2"></i>系统概览</h3>
    <div class="row">
      <div class="col-md-3 mb-3" v-for="stat in stats" :key="stat.label">
        <RouterLink :to="stat.path" class="text-decoration-none text-reset">
          <div class="card text-center h-100">
            <div class="card-body">
              <h5 class="card-title">{{ stat.count }}</h5>
              <p class="card-text text-muted mb-0">{{ stat.label }}</p>
            </div>
          </div>
        </RouterLink>
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
import { useRunningPoll } from '../useRunningPoll.js'

const modules = ref([])
const stats = ref([])
const { schedulePoll } = useRunningPoll(loadModules, modules)

async function loadModules() {
  try {
    const res = await fetch('/api/modules')
    modules.value = await res.json()
    stats.value = [
      { label: 'Alist2Strm', path: '/alist2strm', count: modules.value.filter(m => m.type === 'alist2strm').length },
      { label: 'Ani2Alist', path: '/ani2alist', count: modules.value.filter(m => m.type === 'ani2alist').length },
      { label: '封面生成', path: '/libraryposter', count: modules.value.filter(m => m.type === 'libraryposter').length },
      { label: 'Alist 同步', path: '/alistsync', count: modules.value.filter(m => m.type === 'alistsync').length },
      { label: '递归移动', path: '/filemove', count: modules.value.filter(m => m.type === 'filemove').length },
    ]
  } catch (e) {
    console.error(e)
  }
  schedulePoll()
}

async function triggerRun(m) {
  await fetch(`/api/modules/${m.type}/${m.id}/run`, { method: 'POST' })
  await loadModules()
  schedulePoll()
}

async function toggleModule(m) {
  await fetch(`/api/modules/${m.type}/${m.id}/toggle`, { method: 'POST' })
  loadModules()
}

onMounted(loadModules)
</script>
