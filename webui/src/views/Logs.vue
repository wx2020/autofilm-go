<template>
  <div>
    <h3 class="mb-4"><i class="bi-journal-text me-2"></i>日志</h3>
    <div class="mb-2 d-flex gap-2 align-items-center">
      <button class="btn btn-sm" :class="streaming ? 'btn-danger' : 'btn-success'"
              :disabled="connecting" @click="toggleStream">
        <i :class="streaming ? 'bi-stop' : 'bi-play'"></i>
        {{ streaming ? '停止实时' : (connecting ? '连接中…' : '实时推送') }}
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
    <div v-if="streamError" class="alert alert-danger py-1 mt-2 mb-2" style="font-size: 0.8rem">{{ streamError }}</div>
    <div ref="logBox" class="bg-dark text-light p-3 rounded" style="height: 70vh; overflow-y: auto; font-size: 0.8rem">
      <div v-for="(line, i) in logLines" :key="i" class="font-monospace" :class="lineClass(line)" style="white-space: pre-wrap">{{ line }}</div>
      <div v-if="!logLines.length" class="text-muted">暂无日志</div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'

const logLines = ref([])
const streaming = ref(false)
const connecting = ref(false)
const streamError = ref('')
const levelFilter = ref('')
const logBox = ref(null)
let ws = null
let connectTimer = null
let userStopped = false

function scrollToBottom() {
  const el = logBox.value
  if (el) el.scrollTop = el.scrollHeight
}

// 仅当用户已在底部附近时跟随滚动，避免查看历史时被拽走
function stickToBottom() {
  const el = logBox.value
  if (!el) return
  if (el.scrollHeight - el.scrollTop - el.clientHeight < 80) {
    nextTick(scrollToBottom)
  }
}

async function loadLogs() {
  try {
    let url = '/api/logs?lines=500'
    if (levelFilter.value) url += '&level=' + levelFilter.value
    const res = await fetch(url)
    logLines.value = await res.json()
    nextTick(scrollToBottom)
  } catch (e) { console.error(e) }
}

function clearConnectTimer() {
  if (connectTimer) { clearTimeout(connectTimer); connectTimer = null }
}

// 按行首 [LEVEL] 着色（后端推送/文件历史均为纯文本，无 ANSI 码，大小写兼容）
function lineClass(line) {
  const m = /^\[(\w+)\]/.exec(line || '')
  switch ((m ? m[1] : '').toLowerCase()) {
    case 'error':
    case 'fatal':
    case 'panic':
      return 'text-danger'
    case 'warn':
    case 'warning':
      return 'text-warning'
    case 'debug':
      return 'text-secondary'
    case 'info':
      return 'text-info'
    default:
      return ''
  }
}

function toggleStream() {
  if (streaming.value || connecting.value) {
    userStopped = true
    if (ws) { try { ws.close() } catch (e) {} ws = null }
    clearConnectTimer()
    connecting.value = false
    streaming.value = false
  } else {
    startStream()
  }
}

function startStream() {
  // 防重复建连：先关掉旧 socket
  if (ws) { try { ws.close() } catch (e) {} ws = null }
  userStopped = false
  streamError.value = ''
  connecting.value = true
  streaming.value = false
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const token = sessionStorage.getItem('autofilm_token')
  const query = token ? `?token=${encodeURIComponent(token)}` : ''
  let sock
  try {
    sock = new WebSocket(`${protocol}//${location.host}/api/logs/stream${query}`)
  } catch (e) {
    connecting.value = false
    streamError.value = '实时推送连接失败：' + (e && e.message ? e.message : e)
    return
  }
  ws = sock
  // 8 秒仍未 open 则判定失败，给出可见报错而不是按钮“假死”
  clearConnectTimer()
  connectTimer = setTimeout(() => {
    if (ws === sock && !streaming.value) {
      try { sock.close() } catch (e) {}
      if (ws === sock) ws = null
      connecting.value = false
      streamError.value = '实时推送连接失败：握手超时（可能是登录失效或反代未放行 WebSocket），请刷新页面或检查反代 Upgrade 配置'
    }
  }, 8000)
  sock.onopen = () => {
    if (ws !== sock) { try { sock.close() } catch (e) {} return }
    clearConnectTimer()
    connecting.value = false
    streaming.value = true
  }
  sock.onmessage = (e) => {
    if (ws !== sock) return
    logLines.value.push(e.data)
    if (logLines.value.length > 1000) logLines.value.shift()
    stickToBottom()
  }
  sock.onclose = () => {
    if (ws !== sock) return
    ws = null
    clearConnectTimer()
    connecting.value = false
    streaming.value = false
    // 非手动停止：大概率是 401/反代拦截/网络断开，给出可见报错
    if (!userStopped) {
      streamError.value = '实时推送连接中断：登录可能已失效或网络/反代断开，请重新登录后重试'
    }
  }
  sock.onerror = () => {
    // 具体报错由 onclose 统一呈现，避免重复提示
  }
}

onMounted(loadLogs)
onUnmounted(() => { userStopped = true; if (ws) ws.close() })
</script>
