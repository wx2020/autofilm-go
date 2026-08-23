<template>
  <section class="card mb-4">
    <div class="card-header d-flex align-items-center justify-content-between">
      <strong><i class="bi-sliders me-2"></i>在线配置</strong>
      <button class="btn btn-sm btn-primary" @click="createConfig"><i class="bi-plus-lg me-1"></i>新增配置</button>
    </div>
    <div class="card-body">
      <div v-if="message" class="alert py-2" :class="error ? 'alert-danger' : 'alert-success'">{{ message }}</div>
      <div v-if="configs.length" class="table-responsive">
        <table class="table table-sm align-middle">
          <thead><tr><th>ID</th><th>状态</th><th>Cron</th><th class="text-end">操作</th></tr></thead>
          <tbody><tr v-for="cfg in configs" :key="cfg.id">
            <td>{{ cfg.id }}</td><td><span class="badge" :class="cfg.enable !== false ? 'bg-success' : 'bg-secondary'">{{ cfg.enable !== false ? '启用' : '停用' }}</span></td>
            <td><code>{{ cfg.cron || '-' }}</code></td>
            <td class="text-end"><button class="btn btn-sm btn-outline-primary me-2" @click="editConfig(cfg)"><i class="bi-pencil"></i> 编辑</button><button class="btn btn-sm btn-outline-danger" @click="removeConfig(cfg)"><i class="bi-trash"></i> 删除</button></td>
          </tr></tbody>
        </table>
      </div>
      <div v-else-if="!loading" class="text-muted">暂无配置，可点击“新增配置”。</div>

      <form v-if="editing" class="mt-3 border-top pt-3" @submit.prevent="saveConfig">
        <h6 class="mb-3">基础设置</h6>
        <div class="row g-3">
          <Field label="配置 ID" class="col-md-4"><input v-model.trim="form.id" class="form-control" required :disabled="!!originalID"></Field>
          <Field label="Cron（含秒）" class="col-md-8"><input v-model.trim="form.cron" class="form-control" required placeholder="0 0 */6 * * *"></Field>
          <Switch v-model="form.enable" label="启用任务" />
          <Switch v-model="form.run_on_start" label="启动时立即运行" />
        </div>

        <template v-if="type !== 'libraryposter' && (type !== 'filemove' || form.backend === 'openlist')">
          <h6 class="mt-4 mb-3">Alist / OpenList 连接</h6>
          <div class="row g-3">
            <Field label="服务器地址" class="col-md-6"><input v-model.trim="form.url" type="url" class="form-control" required placeholder="http://127.0.0.1:5244"></Field>
            <Field v-if="type === 'alist2strm'" label="公开访问地址" class="col-md-6"><input v-model.trim="form.public_url" class="form-control" placeholder="可选"></Field>
            <Field label="用户名" class="col-md-4"><input v-model="form.username" class="form-control" autocomplete="username"></Field>
            <Field label="密码" class="col-md-4"><input v-model="form.password" type="password" class="form-control" autocomplete="new-password"></Field>
            <Field label="Token" class="col-md-4"><input v-model="form.token" type="password" class="form-control" autocomplete="new-password"></Field>
            <div class="col-12"><button type="button" class="btn btn-sm btn-outline-info" :disabled="testing" @click="testConnection"><i class="bi-plug me-1"></i>{{ testing ? '测试中…' : '测试连接' }}</button></div>
          </div>
        </template>

        <template v-if="type === 'filemove'">
          <h6 class="mt-4 mb-3">递归移动规则</h6><div class="row g-3">
            <Field label="存储类型" class="col-md-4"><select v-model="form.backend" class="form-select"><option value="local">本地文件系统</option><option value="openlist">OpenList API</option></select></Field>
            <Field label="源目录" class="col-md-6"><input v-model.trim="form.source_dir" class="form-control" required placeholder="D:/downloads"></Field>
            <Field label="目标目录" class="col-md-6"><input v-model.trim="form.target_dir" class="form-control" required placeholder="D:/media"></Field>
            <Field label="正则表达式（匹配相对路径）" class="col-12"><input v-model="form.regex" class="form-control" placeholder="(?i)\\.(mkv|mp4)$"></Field>
            <Field label="精确大小（字节，可选）" class="col-md-4"><input v-model.number="form.size" type="number" min="0" class="form-control" placeholder="不限制"></Field>
            <Field label="最小大小（字节）" class="col-md-4"><input v-model.number="form.min_size" type="number" min="0" class="form-control"></Field>
            <Field label="最大大小（字节，0 不限制）" class="col-md-4"><input v-model.number="form.max_size" type="number" min="0" class="form-control"></Field>
            <Switch v-model="form.overwrite" label="目标存在时覆盖" />
            <div class="col-12"><div class="form-text">文件会保留源目录下的相对目录结构；默认不覆盖目标中的同名文件。</div></div>
          </div>
        </template>

        <template v-else-if="type === 'alist2strm'">
          <h6 class="mt-4 mb-3">扫描与输出</h6><div class="row g-3">
            <Field label="源目录" class="col-md-6"><input v-model.trim="form.source_dir" class="form-control" required></Field>
            <Field label="输出目录" class="col-md-6"><input v-model.trim="form.target_dir" class="form-control" required></Field>
            <Field label="STRM 内容模式" class="col-md-4"><select v-model="form.mode" class="form-select"><option>AlistURL</option><option>RawURL</option><option>AlistPath</option></select></Field>
            <Field label="扫描模式" class="col-md-4"><select v-model="form.scan_mode" class="form-select"><option value="incremental">增量扫描</option><option value="full">全量扫描</option></select></Field>
            <Field label="其他下载后缀" class="col-md-4"><input v-model="form.other_ext" class="form-control" placeholder="例如 ass,srt"></Field>
            <Switch v-model="form.flatten_mode" label="平铺模式" /><Switch v-model="form.subtitle" label="下载字幕" /><Switch v-model="form.image" label="下载图片" /><Switch v-model="form.nfo" label="下载 NFO" /><Switch v-model="form.overwrite" label="覆盖已有文件" /><Switch v-model="form.sync_server" label="同步服务器删除" />
            <Field label="最大扫描并发" class="col-md-3"><input v-model.number="form.max_workers" type="number" min="1" class="form-control"></Field>
            <Field label="最大下载并发" class="col-md-3"><input v-model.number="form.max_downloaders" type="number" min="1" class="form-control"></Field>
            <Field label="QPS 限制" class="col-md-3"><input v-model.number="form.qps_limit" type="number" min="0" class="form-control"></Field>
            <Field label="请求间隔（秒）" class="col-md-3"><input v-model.number="form.wait_time" type="number" min="0" step="0.1" class="form-control"></Field>
            <Field label="同步忽略正则" class="col-12"><input v-model="form.sync_ignore" class="form-control" placeholder="例如 \.(nfo|jpg)$"></Field>
          </div>
          <h6 class="mt-4 mb-3">智能删除保护</h6><div class="row g-3">
            <Switch v-model="form.smart_protection.enabled" label="启用保护" />
            <Field label="触发文件数" class="col-md-4"><input v-model.number="form.smart_protection.threshold" type="number" min="1" class="form-control"></Field>
            <Field label="宽限扫描次数" class="col-md-4"><input v-model.number="form.smart_protection.grace_scans" type="number" min="1" class="form-control"></Field>
          </div>
        </template>

        <template v-else-if="type === 'ani2alist'">
          <h6 class="mt-4 mb-3">动漫订阅</h6><div class="row g-3">
            <Field label="挂载目标目录" class="col-md-6"><input v-model.trim="form.target_dir" class="form-control" required></Field>
            <Switch v-model="form.rss_update" label="启用 RSS 更新" />
            <Field label="年份" class="col-md-3"><input v-model.number="form.year" type="number" min="2000" max="2100" class="form-control" placeholder="不限"></Field>
            <Field label="季度月份" class="col-md-3"><select v-model.number="form.month" class="form-select"><option value="">不限</option><option :value="1">1 月</option><option :value="4">4 月</option><option :value="7">7 月</option><option :value="10">10 月</option></select></Field>
            <Field label="源域名" class="col-md-6"><input v-model.trim="form.src_domain" class="form-control" required></Field>
            <Field label="RSS 域名" class="col-md-6"><input v-model.trim="form.rss_domain" class="form-control" required></Field>
            <Field label="关键词过滤" class="col-12"><input v-model="form.key_word" class="form-control" placeholder="留空表示不过滤"></Field>
          </div>
        </template>

        <template v-else-if="type === 'libraryposter'">
          <h6 class="mt-4 mb-3">Jellyfin / Emby</h6><div class="row g-3">
            <Field label="服务器地址" class="col-md-6"><input v-model.trim="form.url" type="url" class="form-control" required></Field>
            <Field label="API Key" class="col-md-6"><input v-model="form.api_key" type="password" class="form-control" required></Field>
            <Field label="标题字体路径" class="col-md-6"><input v-model="form.title_font_path" class="form-control"></Field>
            <Field label="副标题字体路径" class="col-md-6"><input v-model="form.subtitle_font_path" class="form-control"></Field>
          </div>
          <h6 class="mt-4">媒体库</h6>
          <div v-for="(lib, i) in form.configs" :key="i" class="row g-2 align-items-end border rounded p-2 mt-2">
            <Field label="媒体库名称" class="col-md-3"><input v-model="lib.library_name" class="form-control" required></Field>
            <Field label="标题" class="col-md-3"><input v-model="lib.title" class="form-control"></Field>
            <Field label="副标题" class="col-md-3"><input v-model="lib.subtitle" class="form-control"></Field>
            <Field label="海报数量" class="col-md-2"><input v-model.number="lib.limit" type="number" min="1" class="form-control"></Field>
            <div class="col-md-1"><button type="button" class="btn btn-outline-danger" @click="form.configs.splice(i,1)"><i class="bi-trash"></i></button></div>
          </div>
          <button type="button" class="btn btn-sm btn-outline-primary mt-2" @click="form.configs.push({library_name:'',title:'',subtitle:'',limit:15})"><i class="bi-plus"></i> 添加媒体库</button>
        </template>

        <template v-else-if="type === 'alissync'">
          <h6 class="mt-4">同步目录对</h6>
          <div v-for="(pair, i) in form.pairs" :key="i" class="row g-2 align-items-end border rounded p-2 mt-2">
            <Field label="源目录" class="col-md-4"><input v-model="pair.src" class="form-control" required></Field>
            <Field label="目标目录" class="col-md-4"><input v-model="pair.dst" class="form-control" required></Field>
            <Field label="覆盖策略" class="col-md-2"><select v-model="pair.overwrite" class="form-select"><option value="never">从不</option><option value="always">总是</option><option value="if_newer">源较新时</option></select></Field>
            <Switch v-model="pair.delete_src" label="完成后删源" class="col-md-1" />
            <div class="col-md-1"><button type="button" class="btn btn-outline-danger" @click="form.pairs.splice(i,1)"><i class="bi-trash"></i></button></div>
          </div>
          <button type="button" class="btn btn-sm btn-outline-primary mt-2" @click="form.pairs.push({src:'',dst:'',delete_src:false,overwrite:'if_newer'})"><i class="bi-plus"></i> 添加目录对</button>
          <h6 class="mt-4 mb-3">失败重试</h6><div class="row g-3">
            <Field label="最大尝试次数" class="col-md-4"><input v-model.number="form.retry.max_attempts" type="number" min="1" class="form-control"></Field>
            <Field label="退避策略" class="col-md-4"><select v-model="form.retry.backoff" class="form-select"><option value="expo">指数退避</option></select></Field>
            <Field label="抖动比例" class="col-md-4"><input v-model.number="form.retry.jitter" type="number" min="0" max="1" step="0.1" class="form-control"></Field>
          </div>
        </template>

        <div class="mt-4 d-flex gap-2"><button class="btn btn-primary" :disabled="saving"><i class="bi-save me-1"></i>{{ saving ? '保存中…' : '保存并应用' }}</button><button type="button" class="btn btn-outline-secondary" @click="editing=false">取消</button></div>
      </form>
    </div>
  </section>
</template>

<script setup>
import { defineComponent, h, onMounted, ref } from 'vue'
const Field = defineComponent({ props:{label:String}, setup(p,{slots,attrs}){return()=>h('div',attrs,[h('label',{class:'form-label'},p.label),slots.default?.()])} })
const Switch = defineComponent({ inheritAttrs:false, props:{modelValue:Boolean,label:String}, emits:['update:modelValue'], setup(p,{emit,attrs}){return()=>h('div',{class:attrs.class || 'col-md-3'},[h('div',{class:'form-check form-switch mt-4 pt-2'},[h('input',{class:'form-check-input',type:'checkbox',checked:p.modelValue,onChange:e=>emit('update:modelValue',e.target.checked)}),h('label',{class:'form-check-label'},p.label)])])} })
const props=defineProps({type:{type:String,required:true},defaults:{type:Object,required:true}}); const emit=defineEmits(['changed'])
const configs=ref([]),editing=ref(false),loading=ref(false),saving=ref(false),testing=ref(false),form=ref({}),originalID=ref(''),message=ref(''),error=ref(false)
const clone=v=>JSON.parse(JSON.stringify(v))
function normalize(v){const x=clone(v); x.smart_protection ||= {enabled:true,threshold:100,grace_scans:3}; x.configs ||= []; x.pairs ||= []; x.retry ||= {max_attempts:10,backoff:'expo',jitter:.2}; if(props.type==='filemove'){x.backend ??= 'local'; x.regex ??= ''; x.size ??= null; x.min_size ??= 0; x.max_size ??= 0; x.overwrite ??= false} return x}
async function request(url,options){const res=await fetch(url,options),body=await res.json().catch(()=>({}));if(!res.ok)throw new Error(body.error||`请求失败 (${res.status})`);return body}
async function load(){loading.value=true;try{configs.value=await request(`/api/configs/${props.type}`)}catch(e){show(e.message,true)}finally{loading.value=false}}
function createConfig(){originalID.value='';form.value=normalize(props.defaults);editing.value=true;message.value=''}
function editConfig(cfg){originalID.value=String(cfg.id);form.value=normalize(cfg);editing.value=true;message.value=''}
function validate(){const f=form.value;if(!/^[A-Za-z0-9_-]{2,64}$/.test(f.id||''))return'配置 ID 格式无效';if(!f.cron||f.cron.trim().split(/\s+/).length<5)return'Cron 表达式无效';if(props.type!=='libraryposter'&&props.type!=='filemove'&&!/^https?:\/\//.test(f.url||''))return'Alist 地址必须以 http:// 或 https:// 开头';if(props.type==='filemove'&&(!f.source_dir||!f.target_dir))return'源目录和目标目录不能为空';if(props.type==='alist2strm'&&(!f.source_dir?.startsWith('/')||!f.target_dir))return'源目录必须以 / 开头，输出目录不能为空';if(props.type==='ani2alist'&&!f.target_dir?.startsWith('/'))return'挂载目标目录必须以 / 开头';if(props.type==='libraryposter'&&(!/^https?:\/\//.test(f.url||'')||!f.api_key))return'媒体服务器地址或 API Key 无效';if(props.type==='alissync'&&(!f.pairs?.length||f.pairs.some(p=>!p.src?.startsWith('/')||!p.dst?.startsWith('/'))))return'至少需要一组以 / 开头的同步目录对';return''}
async function saveConfig(){const invalid=validate();if(invalid){show(invalid,true);return} saving.value=true;try{await request(`/api/configs/${props.type}`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(form.value)});editing.value=false;show('配置已保存并应用');await load();emit('changed')}catch(e){show(e.message,true)}finally{saving.value=false}}
async function testConnection(){testing.value=true;try{await request('/api/alist/test',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url:form.value.url,username:form.value.username,password:form.value.password,token:form.value.token})});show('连接测试成功')}catch(e){show(`连接失败：${e.message}`,true)}finally{testing.value=false}}
async function removeConfig(cfg){if(!confirm(`确定删除配置“${cfg.id}”吗？`))return;try{await request(`/api/configs/${props.type}/${encodeURIComponent(cfg.id)}`,{method:'DELETE'});show('配置已删除');await load();emit('changed')}catch(e){show(e.message,true)}}
function show(text,isError=false){message.value=text;error.value=isError} onMounted(load)
</script>
