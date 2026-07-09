<template>
  <div>
    <h3 class="mb-4"><i class="bi-journal-text me-2"></i>日志</h3>
    <div class="mb-2 d-flex gap-2 align-items-center">
      <button class="btn btn-sm" :class="streaming ? 'btn-danger' : 'btn-success'"
              @click="toggleStream">
        <i :class="streaming ? 'bi-stop' : 'bi-play'"></i>
        {{ streaming ? '停止实时' : '实时推送' }}
      </button>
      <button class="btn btn-sm btn-outline-secondary" @click="loadLogs">
        <i class="bi-arrow-clockwise"></i> 刷新
      </button>
      <select v-model="levelFilter" class="form-select form-select-sm" style="width:auto" @change="loadLogs">
        <option value="">全部级别</option>
        <option value="info">INFO</option>
        <option value="warn">WARN</option>
        <option value="error">ERROR</option>
        <option value="debug">DEBUG</option>
      </select>
    </div>
    <div class="bg-dark text-light p-3 rounded" style="height: 70vh; overflow-y: auto; font-size: 0.8rem">
      <div v-for="(line, i) in logLines" :key="i" class="font-monospace" style="white-space: pre-wrap">{{ line }}</div>
      <div v-if="!logLines.length" class="text-muted">暂无日志</div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'

const logLines = ref([])
const streaming = ref(false)
const levelFilter = ref('')
let ws = null

async function loadLogs() {
  try {
    let url = '/api/logs?lines=500'
    if (levelFilter.value) url += '&level=' + levelFilter.value
    const res = await fetch(url)
    logLines.value = await res.json()
  } catch (e) { console.error(e) }
}

function toggleStream() {
  if (streaming.value) {
    if (ws) { ws.close(); ws = null }
    streaming.value = false
  } else {
    startStream()
  }
}

function startStream() {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  ws = new WebSocket(`${protocol}//${location.host}/api/logs/stream`)
  ws.onopen = () => { streaming.value = true }
  ws.onmessage = (e) => {
    logLines.value.push(e.data)
    if (logLines.value.length > 1000) logLines.value.shift()
  }
  ws.onclose = () => { streaming.value = false }
  ws.onerror = () => { streaming.value = false }
}

onMounted(loadLogs)
onUnmounted(() => { if (ws) ws.close() })
</script>
