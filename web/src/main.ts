import { createApp } from 'vue'
import App from './App.vue'
import { dialog } from './directives/dialog'

// ── 全局样式表 ───────────────────────────────────────────────────────────────
// 这些样式表共享同一个全局作用域，**后面的文件覆盖前面的文件**（同等特异度时
// 按出现顺序取胜）。新增样式表请追加到末尾，不要插在中间，否则会改变既有覆盖
// 关系、导致主题错乱。组件专属样式请写在该组件的 <style> 块里，不要加到这里。
//
// 当前分层（由底到顶）：
//   style.css            基础重置、布局骨架、按钮等原子样式
//   usability.css        可读性与触控尺寸（字号、行高、点击区域）
//   feature-layout.css   各功能页的栅格与工具条排布
//   refinement.css       视觉精修与局部覆盖（本层最多，注意累积）
//   theme.css            调色板变量 + html[data-palette] 主题覆盖
//   site-list.css        Web 服务列表
//   forward-list.css     端口转发列表
//   security-policy.css  访问控制与安全策略
//   settings-panel.css   面板设置抽屉
//   operations.css       操作类组件
//   anime-themes.css     二次元主题（壁纸与配色覆盖，必须在 theme.css 之后）
//   traffic-display.css  流量图表
import './style.css'
import './usability.css'
import './feature-layout.css'
import './refinement.css'
import './theme.css'
import './site-list.css'
import './forward-list.css'
import './security-policy.css'
import './settings-panel.css'
import './operations.css'
import './anime-themes.css'
import './traffic-display.css'

createApp(App).directive('dialog', dialog).mount('#app')
