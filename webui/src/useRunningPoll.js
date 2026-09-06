import { onUnmounted } from 'vue'

// 仅当存在运行中模块时周期性刷新，直到全部结束。
// 解决“点击运行后按钮保持禁用、任务结束后自动恢复可点”的状态同步问题。
//
// load: 刷新模块列表的函数；modulesRef: 模块列表 ref；intervalMs: 轮询间隔。
// 用法：
//   const { schedulePoll } = useRunningPoll(load, modules)
//   async function load() { ...; modules.value = ...; schedulePoll() }
export function useRunningPoll(load, modulesRef, intervalMs = 5000) {
  let timer = null
  const stop = () => {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
  }
  const schedule = () => {
    stop()
    const list = modulesRef.value || []
    if (Array.isArray(list) && list.some((m) => m && m.running)) {
      timer = setTimeout(async () => {
        try {
          await load()
        } finally {
          schedule()
        }
      }, intervalMs)
    }
  }
  onUnmounted(stop)
  return { schedulePoll: schedule, stopPoll: stop }
}
