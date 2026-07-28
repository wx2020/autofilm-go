<template>
  <div>
    <h3 class="mb-4"><i class="bi-gear me-2"></i>系统设置</h3>
    <form class="card" @submit.prevent="save">
      <div class="card-header fw-semibold">基础设置</div>
      <div class="card-body row g-3">
        <div class="col-md-6">
          <label class="form-label">时区</label>
          <select v-model="form.timezone" class="form-select">
            <option value="Asia/Shanghai">Asia/Shanghai（中国标准时间）</option>
            <option value="Asia/Hong_Kong">Asia/Hong_Kong</option>
            <option value="Asia/Tokyo">Asia/Tokyo</option>
            <option value="UTC">UTC</option>
          </select>
        </div>
        <div class="col-md-6 d-flex align-items-end">
          <div class="form-check form-switch mb-2">
            <input id="debug" v-model="form.debug" class="form-check-input" type="checkbox">
            <label class="form-check-label" for="debug">调试日志</label>
          </div>
        </div>
      </div>

      <div class="card-header border-top fw-semibold">Web 管理服务</div>
      <div class="card-body row g-3">
        <div class="col-12">
          <div class="form-check form-switch">
            <input id="webEnabled" v-model="form.web_enabled" class="form-check-input" type="checkbox">
            <label class="form-check-label" for="webEnabled">启用 Web 管理界面</label>
          </div>
        </div>
        <div class="col-md-6">
          <label class="form-label">监听地址</label>
          <select v-model="form.web_host" class="form-select">
            <option value="127.0.0.1">127.0.0.1（仅本机，推荐）</option>
            <option value="0.0.0.0">0.0.0.0（局域网/容器）</option>
          </select>
        </div>
        <div class="col-md-6">
          <label class="form-label">端口</label>
          <input v-model.number="form.web_port" type="number" min="1" max="65535" class="form-control" required>
        </div>
        <div class="col-12">
          <label class="form-label">访问令牌</label>
          <input v-model="form.web_token" type="password" class="form-control" autocomplete="new-password" placeholder="留空表示不启用 API 鉴权">
          <div class="form-text">监听 0.0.0.0 时强烈建议设置。可通过 <code>?token=令牌</code> 首次进入界面。</div>
        </div>
        <div class="col-12">
          <label class="form-label">告警 Webhook</label>
          <input v-model="form.alert_webhook" type="url" class="form-control" placeholder="任务失败时 POST JSON，可留空">
        </div>
      </div>

      <div class="card-footer d-flex align-items-center gap-3">
        <button class="btn btn-primary" :disabled="saving"><i class="bi-save me-2"></i>{{ saving ? '保存中…' : '保存设置' }}</button>
        <span v-if="message" :class="error ? 'text-danger' : 'text-success'">{{ message }}</span>
      </div>
    </form>
    <div class="alert alert-secondary mt-3 mb-0"><i class="bi-database me-2"></i>系统和模块配置均保存在 SQLite，不再使用 config.yaml。</div>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'

const form = reactive({ debug: false, timezone: 'Asia/Shanghai', web_enabled: true, web_host: '127.0.0.1', web_port: 8080, web_token: '', alert_webhook: '' })
const saving = ref(false)
const message = ref('')
const error = ref(false)

async function load() {
  try {
    const res = await fetch('/api/config')
    const data = await res.json()
    if (!res.ok) throw new Error(data.error || '读取失败')
    Object.assign(form, data)
  } catch (e) { error.value = true; message.value = e.message }
}

async function save() {
  saving.value = true; message.value = ''
  try {
    const res = await fetch('/api/config', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(form) })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error || '保存失败')
    error.value = false
    message.value = data.restart_required ? '已保存；Web 地址、端口和鉴权设置将在重启后生效。' : '已保存。'
  } catch (e) { error.value = true; message.value = e.message }
  finally { saving.value = false }
}

onMounted(load)
</script>
