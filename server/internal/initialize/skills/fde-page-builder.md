# FDE 页面生成

在 FDE Vue 3 + TypeScript 后台中新增或修改列表、表单、详情、树表和统计组合页面时使用。沿用项目布局、Element Plus、vxe-table 及已有组件；营销站点和移动端使用各自项目规范。

## 从需求到页面

先核对已有页面、组件、API 和当前变更，整理同一份最小业务说明：实体/字段 key 与类型、默认值、查询条件、页面区块、操作 key、状态与可用条件、角色和数据归属、接口输入输出、图表指标及验收路径。页面与业务逻辑沿用这些稳定 key，按钮文字仅用于显示。影响业务结果的缺失信息需要澄清，布局、间距和配色默认跟随项目；已有页面按需求局部修改，保留人工代码和无关交互。

列表、编辑及统计共用明确的筛选与字段语义。列表分页参数不进入全量统计；数据来源是示例时显式标注。普通操作补齐加载、空数据、无权限、失败及重试；提交失败保留用户输入，成功后再关闭表单并刷新相关区域。

## 组件选择与实际契约

- **列表/树表**：`import { TablePage } from '@/components/table-page'`，类型从同一入口导入。必需属性 `columns`、`fetch`；`fetch(query)` 接收 `{page, pageSize, ...filters}`（页码从 1 开始），返回 `Promise<{list, total}>`。通过 `searchFields`、`treeData`、`treeNodeKey`、`treeQueryField`、`toolbarActions` 配置查询、树和工具栏，列插槽使用 `columns[].slots.default` 指定名称。事件为 `search/reset/add/action/tree-change`，操作 code 使用稳定标识。组件内部负责搜索/树筛选后的分页重置，不再手写另一套分页或重复触发加载。ref 公开 `reload()/zoom()/getGrid()`；TablePage 的 `reload()` 不接收参数。外部筛选可由 fetch 适配器合并，改变后使用实际分页接口重置，先检查源码。
- **Schema 表单**：默认导入 `FormRender` 自 `@/components/form-render/FormRender.vue`；`FormSchema` 类型来自 `@/components/form-render/types`。用 `<FormRender :schema="schema" v-model="model" :disabled="saving" />`，属性还有 `labelWidth`，值更新事件仅 `update:modelValue`。Schema 为 `{objectKey, name, fields: {fieldKey: {type, title, required, 'x-ui': {...}}}, 'x-linkages': [...]}`，必填是字段内的布尔值。`x-ui.control` 支持 input/textarea/number/switch/datetime/select/radio/table-select，布局用 colSpan。页面显式初始化默认值、加载编辑值，提交时按 DTO 白名单映射。
- **表单边界**：FormRender 公开 `validate()` 和 `clearValidate()`。提交前等待 `validate()` 成功，默认值由页面初始化到 model。专用表单可沿用 Element Plus 的整体验证。联动只支持 `when: {field, eq}` → `then: {field, visible?, required?}`。公式、赋值、跨字段和远程级联用页面逻辑实现；隐藏字段是否提交须由业务规则决定。`table-select` 只是本地选项过滤单选，远程关联选择需要真实接口与实现。静态 `x-ui.options` 优先于 enum，再到字典；当前字典接口用数字 typeId，编码字符串的解析能力需另行实现和验证。
- **统计图表**：从 `@/components/charts` 导入 `BaseChart/createLineOption/createBarOption/createDonutOption`。BaseChart 接收完整 `option`、`height/loading/empty/error/ariaLabel`，事件 `click/ready/retry`；组件管理实例和尺寸，页面管理请求、错误和筛选。折线/柱状工厂输入 `{categories: string[], series: {name: string, data: (number|null)[]}[], valueName?, valueFormatter?}`；环形输入 `{data: {name, value}[], seriesName?, valueFormatter?}`。趋势用折线、分类对比用柱状、非负可相加构成用饼/环形，其他需求按官方示例扩展 option；新图表模块通过 `registerCharts` 按需注册。统计来自后端全量聚合接口，明确金额单位、退款、时区、补零及百分比分母；只转换一次单位，真实 0 与无数据分别判断。请求竞态以最新筛选结果为准，点击联动使用真实维度 ID。
- **其他区块**：详情、弹窗、上传和专用交互优先组合现有 Element Plus 组件及项目 API。按钮可见性使用现有权限数据判断；服务端仍独立鉴权。先读同类实现，不假设存在未核实的权限指令、通用详情组件或关联选择器。

前端 API 在 `web/src/api` 定义类型并复用 `@/utils/request`。成功结果已解包为 data，列表直接取 `{list,total}`；请求路径相对于 `/api`，不重复拼前缀、不读 `result.data.data`。错误提示和登录续期已由拦截器处理，页面捕获异常维护本地状态，避免重复弹错。Schema 工具字段 key 用小写 snake_case，接口 JSON 按既有 camelCase 约定，显式维护编辑回填/提交的双向映射。普通时间字段遵循后端 RFC3339，业务日查询参数遵循其日期接口契约。

## 执行边界与验收

根据当前入口提供的工具执行。普通会话中的 `generate_form_schema` 生成 Schema 草稿，`generate_business_code` 返回基础 CRUD 预览，写入由用户在应用中确认批次。开发工作台的 `generate_business_code` 在本次任务工作区生成基础单表模块，使用共享 FormRender 和业务安装资产。根据实际权限使用 `read_file`、`apply_patch` 和 `run_checks` 核验代码、修复与检查结果。已有业务的跨任务修改需要版本化增量流程。

只传工具公开 Schema 中已声明的参数并核对结果。Schema 保存会替换字段集合，`mode:update` 不代表局部合并；更新前读取完整已有对象并合并保留原字段/规则，保存后重新读取核对。代码生成 dry-run 仍会写生成审计，“不写源码”不等同于完全只读；不能在只读任务中默认调用。

交付关联业务说明、文件/API/菜单权限变更和实际验证结果。核验查询重置/分页、默认值与编辑回填、提交前验证、失败保留输入、成功刷新、权限、统计口径及图表缩放/重复进入。使用项目现有类型检查、构建和可用的浏览器工具；没有编辑/运行/预览能力时交付方案或代码并列出待执行项。技能绑定、文本生成与构建部署分别报告。

开发环境参考：`docs/components/table-page.md`、`docs/components/charts.md`、`docs/api-conventions.md`、`web/src/components/form-render/types.ts`；图表配置参考 https://echarts.apache.org/examples/zh/index.html 。关键契约已写在本技能中，文件读取仍取决于实际工具。
