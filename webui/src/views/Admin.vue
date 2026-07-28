<template>
  <div>
    <h3 class="mb-4"><i class="bi-shield-lock me-2"></i>用户与数据管理</h3>
    <div v-if="message" class="alert py-2" :class="error?'alert-danger':'alert-success'">{{ message }}</div>
    <section class="card mb-4">
      <div class="card-header d-flex justify-content-between"><strong>用户和角色</strong><button class="btn btn-sm btn-primary" @click="showNew=!showNew">新增用户</button></div>
      <div class="card-body">
        <form v-if="showNew" class="row g-2 mb-3" @submit.prevent="createUser">
          <div class="col-md-3"><input v-model="draft.username" class="form-control" placeholder="用户名" minlength="3" required></div>
          <div class="col-md-3"><input v-model="draft.password" type="password" class="form-control" placeholder="密码（至少8位）" minlength="8" required></div>
          <div class="col-md-3"><select v-model="draft.role" class="form-select"><option value="viewer">只读用户</option><option value="operator">操作员</option><option value="admin">管理员</option></select></div>
          <div class="col-md-3"><button class="btn btn-success">创建</button></div>
        </form>
        <table class="table align-middle"><thead><tr><th>用户名</th><th>角色</th><th>启用</th><th>操作</th></tr></thead><tbody>
          <tr v-for="u in users" :key="u.id"><td>{{ u.username }}</td><td><select v-model="u.role" class="form-select form-select-sm"><option value="viewer">只读</option><option value="operator">操作员</option><option value="admin">管理员</option></select></td><td><input v-model="u.enabled" type="checkbox" class="form-check-input"></td><td><button class="btn btn-sm btn-outline-primary me-2" @click="saveUser(u)">保存</button><button class="btn btn-sm btn-outline-danger" @click="deleteUser(u)">删除</button></td></tr>
        </tbody></table>
      </div>
    </section>
    <section class="card">
      <div class="card-header"><strong>备份与恢复</strong></div>
      <div class="card-body d-flex gap-2 align-items-center">
        <button class="btn btn-outline-primary" @click="downloadBackup"><i class="bi-download me-1"></i>下载配置备份</button>
        <label class="btn btn-outline-warning mb-0"><i class="bi-upload me-1"></i>恢复备份<input type="file" accept=".json,application/json" hidden @change="restoreBackup"></label>
      </div>
    </section>
  </div>
</template>
<script setup>
import { onMounted, reactive, ref } from 'vue'
const users=ref([]),showNew=ref(false),draft=reactive({username:'',password:'',role:'viewer'}),message=ref(''),error=ref(false)
async function req(url,opt){const r=await fetch(url,opt),d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||'请求失败');return d}
async function load(){try{users.value=await req('/api/users')}catch(e){show(e.message,true)}}
function show(m,e=false){message.value=m;error.value=e}
async function createUser(){try{await req('/api/users',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(draft)});Object.assign(draft,{username:'',password:'',role:'viewer'});showNew.value=false;show('用户已创建');load()}catch(e){show(e.message,true)}}
async function saveUser(u){try{await req(`/api/users/${u.id}`,{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({role:u.role,enabled:u.enabled})});show('用户已更新')}catch(e){show(e.message,true)}}
async function deleteUser(u){if(!confirm(`删除用户 ${u.username}？`))return;try{await req(`/api/users/${u.id}`,{method:'DELETE'});show('用户已删除');load()}catch(e){show(e.message,true)}}
async function downloadBackup(){const r=await fetch('/api/backup');if(!r.ok){show('备份失败',true);return}const blob=await r.blob(),a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download=`autofilm-backup-${new Date().toISOString().slice(0,10)}.json`;a.click();URL.revokeObjectURL(a.href)}
async function restoreBackup(e){const file=e.target.files[0];if(!file||!confirm('恢复会覆盖当前系统和模块配置，是否继续？'))return;try{await req('/api/restore',{method:'POST',headers:{'Content-Type':'application/json'},body:await file.text()});show('备份已恢复，任务已重载')}catch(err){show(err.message,true)}finally{e.target.value=''}}
onMounted(load)
</script>
