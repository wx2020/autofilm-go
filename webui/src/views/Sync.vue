<template>
  <div>
    <h3 class="mb-4"><i class="bi-arrow-left-right me-2"></i>同步任务队列</h3>
    <div class="table-responsive">
      <table class="table table-hover">
        <thead>
          <tr>
            <th>源路径</th>
            <th>目标路径</th>
            <th>状态</th>
            <th>尝试次数</th>
            <th>错误信息</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="t in tasks" :key="t.id">
            <td class="text-truncate" style="max-width:200px">{{ t.src_path }}</td>
            <td class="text-truncate" style="max-width:200px">{{ t.dst_path }}</td>
            <td>
              <span class="badge" :class="stateBadge(t.state)">{{ t.state }}</span>
            </td>
            <td>{{ t.attempts }}</td>
            <td class="text-truncate text-danger" style="max-width:200px">{{ t.last_error }}</td>
            <td>
              <button class="btn btn-sm btn-outline-primary" @click="retry(t.id)"
                      :disabled="t.state === 'running' || t.state === 'succeeded'">
                <i class="bi-arrow-repeat"></i> 重试
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="!tasks.length" class="text-muted">暂无同步任务</div>
    <ModuleCard v-for="m in syncModules" :key="m.id" :module="m"
      @run="triggerRun(m)" @toggle="toggleModule(m)" />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import ModuleCard from '../components/ModuleCard.vue'

const tasks = ref([])
const syncModules = ref([])

function stateBadge(state) {
  return {
    pending: 'bg-secondary',
    running: 'bg-primary',
    succeeded: 'bg-success',
    failed: 'bg-warning text-dark',
    dead_letter: 'bg-danger'
  }[state] || 'bg-secondary'
}

async function load() {
  try {
    const [tRes, mRes] = await Promise.all([
      fetch('/api/sync/queue'),
      fetch('/api/modules')
    ])
    tasks.value = await tRes.json()
    const all = await mRes.json()
    syncModules.value = all.filter(m => m.type === 'alissync')
  } catch (e) { console.error(e) }
}

async function retry(tid) {
  await fetch(`/api/sync/queue/retry/${tid}`, { method: 'POST' })
  load()
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
