<template>
  <div>
    <h3 class="mb-4"><i class="bi-activity me-2"></i>监控与告警</h3>
    <div class="row mb-4"><div class="col-md-3" v-for="s in stats" :key="s.label"><div class="card text-center"><div class="card-body"><h4>{{ s.value }}</h4><span class="text-muted">{{ s.label }}</span></div></div></div></div>
    <section class="card mb-4"><div class="card-header"><strong>告警</strong></div><div class="card-body table-responsive"><table class="table"><thead><tr><th>时间</th><th>级别</th><th>来源</th><th>消息</th><th></th></tr></thead><tbody><tr v-for="a in alerts" :key="a.id"><td>{{ fmt(a.created_at) }}</td><td><span class="badge bg-danger">{{ a.level }}</span></td><td>{{ a.source }}</td><td>{{ a.message }}</td><td><button v-if="!a.acknowledged" class="btn btn-sm btn-outline-secondary" @click="ack(a)">确认</button><span v-else class="text-success">已确认</span></td></tr></tbody></table></div></section>
    <section class="card"><div class="card-header"><strong>最近运行</strong></div><div class="card-body table-responsive"><table class="table"><thead><tr><th>模块</th><th>配置</th><th>开始时间</th><th>状态</th><th>错误</th></tr></thead><tbody><tr v-for="r in runs" :key="r.id"><td>{{ r.module_type }}</td><td>{{ r.config_uid }}</td><td>{{ fmt(r.started_at) }}</td><td><span class="badge" :class="r.status==='success'?'bg-success':r.status==='running'?'bg-primary':'bg-danger'">{{ r.status }}</span></td><td>{{ r.error_summary }}</td></tr></tbody></table></div></section>
  </div>
</template>
<script setup>
import { computed,onMounted,ref } from 'vue'
const metrics=ref({runs:{},failures:{},running:0,uptime_seconds:0}),alerts=ref([]),runs=ref([])
const stats=computed(()=>[{label:'运行中',value:metrics.value.running||0},{label:'累计执行',value:Object.values(metrics.value.runs||{}).reduce((a,b)=>a+b,0)},{label:'累计失败',value:Object.values(metrics.value.failures||{}).reduce((a,b)=>a+b,0)},{label:'运行时长',value:`${Math.floor((metrics.value.uptime_seconds||0)/3600)}h`}])
const fmt=v=>v?new Date(v).toLocaleString():'-'
async function load(){const [m,a,r]=await Promise.all([fetch('/api/metrics'),fetch('/api/alerts'),fetch('/api/runs')]);if(m.ok)metrics.value=await m.json();if(a.ok)alerts.value=await a.json();if(r.ok)runs.value=await r.json()}
async function ack(a){await fetch(`/api/alerts/${a.id}/ack`,{method:'POST'});load()}
onMounted(()=>{load();setInterval(load,10000)})
</script>
