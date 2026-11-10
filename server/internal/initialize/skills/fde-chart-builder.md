# FDE 图表生成

在 FDE Vue 3 + TypeScript 后台中生成或修改统计图表时使用。趋势优先折线图，分类对比优先柱状图，非负、可相加的构成优先饼/环形图；出现负数或类别过多时改用对比图并说明原因。

先从需求和已有代码确定指标、维度、时间范围及业务时区、筛选条件、金额/数量单位、退款口径、数据权限和缺失日期的含义。影响统计结果的口径不明确时针对缺失项提问；视觉细节沿用项目主题。需求、字段、接口、图表和验收条件保持一致。

## 页面与数据

- 复用 `@/components/charts` 导出的 `BaseChart`、`createLineOption`、`createBarOption`、`createDonutOption`。`BaseChart` 接收 `option`、`height`、`loading`、`empty`、`error`、`ariaLabel`，支持 `click`、`retry`、`ready` 事件。组件负责图表生命周期，页面负责请求和数据映射。
- 折线/柱状配置接收 `{categories: string[], series: {name: string, data: (number|null)[]}[], valueName?, valueFormatter?}`；环形配置接收 `{data: {name: string, value: number}[], seriesName?, valueFormatter?}`。需要额外配置时保留完整 ECharts `option` 能力；先核对组件源码和当前版本文档。
- 示例：`import { BaseChart, createLineOption } from '@/components/charts'`；`createLineOption({ categories: result.trend.map(row => row.date), series: [{ name: '净销售额', data: result.trend.map(row => row.netCents / 100) }], valueName: '元' })`。示例接口金额单位是分，显示元时只转换一次。
- 统计使用后端按筛选条件聚合的全量数据接口。分页列表仅代表当前页，不能据此计算全量统计。后端执行数据权限、日期分组/排序和业务要求的补零；未知/缺失值与真实零值分别处理，零金额不等于没有数据。金额运算使用整数最小货币单位或项目既有精确类型。
- 筛选后重新查询并防止旧请求覆盖新结果。提供加载、空数据、无权限、请求失败与重试状态；失败信息不能伪装成正常统计，保留筛选输入。点击图表联动列表时传递真实维度标识。
- 参考 https://echarts.apache.org/examples/zh/index.html 的配置与当前官方文档，替换示例数据并沿用项目配色。官方示例用于图形配置参考；业务数据、初始化与事件清理由项目组件负责。开发环境可查 `docs/components/charts.md` 与 `web/src/views/demo/charts/index.vue`。

## 交付与验证

新增接口按项目响应信封、JWT/Casbin、菜单/按钮规则接入。修改已有页面时局部调整，保留已有交互与人工代码。示例数据要显式标注来源，真实业务数据通过相应接口获得。

使用当前环境实际提供的工具完成代码变更与检查，记录运行结果；缺少文件编辑、构建、浏览器或发布能力时，明确说明当前产物及未验证项。文本回复、生成配置或保存技能本身不表示页面已经接入、测试通过或完成部署。

验收覆盖筛选与清空、数据和单位映射、空态与错误重试、容器缩放/标签恢复、重复进入离开页面，以及各图表与汇总的统计口径一致；通过项目类型检查和构建后再报告对应结果。
