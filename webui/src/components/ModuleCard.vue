<template>
  <div class="card mb-3">
    <div class="card-body">
      <div class="d-flex justify-content-between align-items-start">
        <div>
          <h5 class="card-title mb-1">{{ module.id }}</h5>
          <span class="badge" :class="module.enabled ? 'bg-success' : 'bg-secondary'">
            {{ module.enabled ? '已启用' : '已禁用' }}
          </span>
          <span class="badge bg-info ms-2">{{ module.type }}</span>
        </div>
        <div class="d-flex gap-2">
          <button class="btn btn-sm" :class="module.enabled ? 'btn-warning' : 'btn-success'"
                  @click="$emit('toggle')">
            <i :class="module.enabled ? 'bi-pause' : 'bi-play'"></i>
          </button>
          <button class="btn btn-sm btn-primary" @click="$emit('run')" :disabled="!module.enabled">
            <i class="bi-play-fill"></i> 运行
          </button>
        </div>
      </div>
      <div class="mt-2 small text-muted">
        <div v-if="module.cron">Cron: <code>{{ module.cron }}</code></div>
        <div v-if="nextRun">下次运行: {{ formatTime(nextRun) }}</div>
        <div>上次运行: {{ lastRun ? formatTime(lastRun) : '未运行' }}</div>
        <div v-if="lastError" class="text-danger">上次错误: {{ lastError }}</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  module: Object
})
defineEmits(['run', 'toggle'])

const nextRun = computed(() => props.module.next_run ? new Date(props.module.next_run) : null)
const lastRun = computed(() => props.module.last_run ? new Date(props.module.last_run) : null)
const lastError = computed(() => props.module.last_error)

function formatTime(d) {
  return d.toLocaleString('zh-CN')
}
</script>
