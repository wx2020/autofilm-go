<template>
  <div class="min-vh-100 d-flex align-items-center justify-content-center bg-dark">
    <form class="card shadow p-4" style="width: 380px" @submit.prevent="submit">
      <h4 class="mb-3"><i class="bi-film me-2"></i>{{ initialized ? '登录 AutoFilm' : '创建管理员' }}</h4>
      <div v-if="error" class="alert alert-danger py-2">{{ error }}</div>
      <label class="form-label">用户名</label>
      <input v-model.trim="username" class="form-control mb-3" minlength="3" required autocomplete="username">
      <label class="form-label">密码</label>
      <input v-model="password" type="password" class="form-control mb-3" minlength="8" required autocomplete="current-password">
      <button class="btn btn-primary w-100" :disabled="loading">{{ loading ? '处理中…' : (initialized ? '登录' : '初始化系统') }}</button>
    </form>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
const router=useRouter(),initialized=ref(true),username=ref(''),password=ref(''),error=ref(''),loading=ref(false)
onMounted(async()=>{const r=await fetch('/api/auth/status');initialized.value=(await r.json()).initialized})
async function submit(){loading.value=true;error.value='';try{const path=initialized.value?'/api/auth/login':'/api/auth/bootstrap';const r=await fetch(path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:username.value,password:password.value})});const d=await r.json();if(!r.ok)throw new Error(d.error||'操作失败');sessionStorage.setItem('autofilm_token',d.token);sessionStorage.setItem('autofilm_user',JSON.stringify(d.user));await router.push('/')}catch(e){error.value=e.message}finally{loading.value=false}}
</script>
