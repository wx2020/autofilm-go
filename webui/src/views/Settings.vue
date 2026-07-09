<template>
  <div>
    <h3 class="mb-4"><i class="bi-gear me-2"></i>系统设置</h3>
    <div class="alert alert-info">
      <i class="bi-info-circle me-2"></i>编辑配置文件并保存后将触发热重载，所有定时任务将自动重建。
    </div>
    <div class="mb-3">
      <label class="form-label">配置文件 (config.yaml)</label>
      <textarea v-model="configRaw" class="form-control font-monospace" rows="30"></textarea>
    </div>
    <button class="btn btn-primary" @click="saveConfig" :disabled="saving">
      <i class="bi-save me-2"></i>{{ saving ? '保存中...' : '保存配置' }}
    </button>
    <div v-if="msg" class="mt-2 alert" :class="msgType">{{ msg }}</div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'

const configRaw = ref('')
const saving = ref(false)
const msg = ref('')
const msgType = ref('')

function showMsg(text, type = 'alert-success') {
  msg.value = text
  msgType.value = type
  setTimeout(() => { msg.value = '' }, 5000)
}

async function loadConfig() {
  try {
    const res = await fetch('/api/config')
    const data = await res.json()
    configRaw.value = data.raw || ''
  } catch (e) {
    showMsg('读取配置失败: ' + e.message, 'alert-danger')
  }
}

async function saveConfig() {
  saving.value = true
  try {
    const res = await fetch('/api/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ raw: configRaw.value })
    })
    const data = await res.json()
    if (data.success) {
      showMsg('配置已保存，定时任务已重建')
    } else {
      showMsg('保存失败: ' + (data.error || '未知错误'), 'alert-danger')
    }
  } catch (e) {
    showMsg('保存失败: ' + e.message, 'alert-danger')
  } finally {
    saving.value = false
  }
}

onMounted(loadConfig)
</script>
