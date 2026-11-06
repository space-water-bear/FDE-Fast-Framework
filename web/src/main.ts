import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import VxeUITable from 'vxe-table'
import VxeUI from 'vxe-pc-ui'
import 'element-plus/dist/index.css'
import 'vxe-pc-ui/lib/style.css'
import 'vxe-table/lib/style.css'

import App from './App.vue'
import router from './router'
import './styles/index.css'

const app = createApp(App)

app.use(createPinia())
app.use(router)
app.use(ElementPlus, { locale: zhCn })
app.use(VxeUI)
app.use(VxeUITable)

for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component as any)
}

app.mount('#app')
