# 表格页通用组件 TablePage 开发文档

> 目标：把「左侧分类树 + 右侧搜索表单 + vxe-grid 表格」这类高频后台页面抽象成可复用组件，
> 业务页面只需要声明 搜索字段 / 列定义 / 数据请求函数，不再重复写模板与分页逻辑。
>
> 在线示例：系统管理 -> 表格组件示例（带左侧树）；系统管理 -> 用户管理（无树）。
> 角色管理 / 操作日志 / 文件管理 / 登录设备 也已迁移到本组件（纯表格或自定义工具条用法）。
> 截图：docs/screenshots/table-page-user.png

## 1. 设计目标

- 一处定义、多处复用：搜索区、工具条、表格、分页由组件统一渲染
- 与 vxe-grid 深度对接：列配置(columns)、单元格插槽(slots)、proxy 远程分页、zoom 全屏全部透传
- 业务页面零表格样板代码：只保留「业务列插槽 + 弹窗表单」
- 可组合：树/搜索/表格均可单独使用，也可用 TablePage 一次组装

## 2. 组件结构

    +--------------------------------------------------------------+
    | TablePage                                                    |
    | +----------------+  +--------------------------------------+  |
    | | TablePageTree  |  | TablePageSearch  (搜索表单)           |  |
    | |  分类树 / 部门  |  | 输入框 选择器 日期范围 + 查询/重置     |  |
    | |  (可选)        |  +--------------------------------------+  |
    | |                |  | TablePageGrid                         |  |
    | |                |  |  [新增][刷新]      [全屏][自定义工具]  |  |
    | |                |  |  vxe-grid (proxy 远程分页 + 列插槽)   |  |
    | |                |  |  分页条                                |  |
    | +----------------+  +--------------------------------------+  |
    +--------------------------------------------------------------+

组件目录：web/src/components/table-page/

    index.ts              统一导出
    types.ts              类型定义(搜索字段/工具按钮/查询/数据源)
    TablePage.vue         组合组件(左树 + 右搜索 + 右表格)
    TablePageSearch.vue   搜索表单(config 驱动)
    TablePageGrid.vue     vxe-grid 封装(工具条 + proxy + 分页 + 插槽透传)
    TablePageTree.vue     左侧分类树(带本地搜索/全部选项)

## 3. 组件 API

### 3.1 TablePage（组合组件，推荐入口）

    props:
      searchFields: SearchField[]           搜索表单字段配置
      columns: any[]                        vxe-grid 列配置(支持 slots 单元格插槽)
      fetch: (query) => Promise<{list,total}>  数据请求函数(远程分页)
      treeData?: any[]                      左侧树数据(不传则不渲染左栏)
      treeLabelKey?: string                 默认 'label'
      treeNodeKey?: string                  默认 'id'
      treeTitle?: string                    树标题(默认不显示)
      treeWidth?: number                    左栏宽度，默认 260
      treeSearchable?: boolean              树节点搜索框，默认 true
      treeDefaultExpandAll?: boolean        默认 true
      treeQueryField?: string               树选中值合并到查询参数的字段名，默认 'treeId'
      toolbarActions?: ToolbarAction[]      左侧工具按钮，默认 [{code:'add',label:'新增'}]；右侧刷新/最大化/列设置为 vxe 内置工具
      pageSize?: number                     默认 20
      autoLoad?: boolean                    挂载后自动请求，默认 true
      gridHeight?: number | string          表格高度，默认 'auto'(撑满容器)

    events:
      search(query)          点击查询
      reset                  点击重置
      add                    点击「新增」按钮(或 toolbarActions 中 code=add)
      action(code)           点击工具条其他按钮
      tree-change(node|null) 左侧树选择变化(null 表示「全部」)

    slots:
      toolbar                工具条左侧额外按钮
      tools                  工具条右侧额外工具
      <column-slot>          任意单元格插槽，按 columns[].slots.default 名称透传到 vxe-grid

    expose:
      reload()               重新请求当前页
      zoom()                 全屏切换
      getGrid()              获取 vxe-grid 实例(高级用法)

### 3.2 TablePageSearch（搜索表单）

    props:
      fields: SearchField[]

    SearchField:
      field: string          参数名(必填)
      label: string          标签(必填)
      type?: 'input' | 'number' | 'select' | 'daterange' | 'datetimerange'(默认 input)
      placeholder?: string
      options?: {label,value}[]         select 选项
      defaultValue?: any                初始值/重置值
      width?: number                    控件宽度(px)
      clearable?: boolean               默认 true

    events:
      search(payload)        点击查询，payload 为全部字段当前值
      reset(payload)         点击重置(已恢复 defaultValue)

### 3.3 TablePageGrid（表格 + 工具条）

    props:
      columns: any[]                        vxe-grid 列配置
      fetch: (query) => Promise<{list,total}>
      queryParams?: Record<string, any>     外部查询参数(变化时自动重置到第一页并刷新)
      toolbarActions?: ToolbarAction[]      左侧按钮，默认 [add]；右侧为 vxe 内置工具(刷新/最大化/列设置)
      pageSize?: number                     默认 20
      autoLoad?: boolean                    默认 true
      height?: number | string              默认 'auto'

    ToolbarAction:
      code: string           按钮标识(code=add/refresh 有内置行为)
      label: string
      type?: 'primary' | 'success' | 'warning' | 'danger' | 'info'
      hidden?: boolean

    events:
      add / action(code) / refresh

    expose:
      reload(keepPage?: boolean)  重新查询(keepPage=false 重置到第一页)
      zoom()                      全屏切换
      getGrid()                   vxe-grid 实例

### 3.4 TablePageTree（左侧分类树）

    props:
      data: any[]            树数据(children 递归)
      labelKey?: string      默认 'label'
      childrenKey?: string   默认 'children'
      nodeKey?: string       默认 'id'
      title?: string
      showSearch?: boolean   默认 true
      defaultExpandAll?: boolean 默认 true

    events:
      node-click(node|null)  选择节点；点击「全部」时传 null

## 4. 与 vxe-grid 的对接说明

本组件对 vxe-grid 的封装遵循 vxe 官方约定（https://vxetable.cn/#/grid/api）：

- 远程分页：内部启用 proxyConfig.enabled + proxyConfig.ajax.query，
  把 vxe 的 page.currentPage/pageSize 转成 fetch 的 query.page/pageSize，
  返回值 {list,total} 通过 proxyConfig.response = { result:'list', total:'total' } 映射
  (vxe 4.x 在启用分页时列表字段读 response.result，total 读 response.total)；
  proxyConfig.autoLoad 由 autoLoad 控制。
- 列插槽：columns[].slots.default 指定插槽名，组件把父级同名插槽原样转发给 vxe-grid，
  业务页面只需写 <template #status="{ row }">…</template>。
- 工具条：使用 vxe-grid 原生 toolbarConfig：
  左侧按钮由 toolbarActions 映射为原生 buttons({code,content,status})，
  右侧为内置工具 刷新(refreshOptions.code='query' 保持当前页)/最大化(zoom)/个性化列(custom)；
  自定义内容通过原生 slots 注入：#toolbar -> buttonSuffix，#tools -> toolSuffix。
  原生左侧按钮点击经 toolbar-button-click 事件转成组件事件(add/action)。
- 分页：pagerConfig 内置 layouts(Total/Sizes/PrevPage/Number/NextPage) 与 pageSizes 20/50/100，
  默认每页 20 条。
- 自适应：autoResize 开启，height='auto' 时随父容器高度自适应(父容器需设定高度或 flex 布局)。

## 5. 使用示例

### 5.1 最小用法（无左侧树，示例：用户管理）

    <template>
      <div class="page">
        <table-page
          :columns="columns"
          :fetch="fetchUsers"
          :search-fields="searchFields"
          @add="openDialog()"
        >
          <template #status="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
              {{ row.enabled ? '启用' : '禁用' }}
            </el-tag>
          </template>
          <template #ops="{ row }">
            <el-button link type="primary" @click="openDialog(row)">编辑</el-button>
            <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
          </template>
        </table-page>
      </div>
    </template>

    <script setup lang="ts">
    import { TablePage } from '@/components/table-page'
    import type { SearchField, TableQuery } from '@/components/table-page'

    const searchFields: SearchField[] = [
      { field: 'keyword', label: '关键词', placeholder: '用户名 / 昵称' },
    ]

    const columns = [
      { type: 'seq', title: '#', width: 60 },
      { field: 'username', title: '用户名', minWidth: 120 },
      { title: '状态', width: 90, slots: { default: 'status' } },
      { title: '操作', width: 160, fixed: 'right', slots: { default: 'ops' } },
    ]

    async function fetchUsers(query: TableQuery) {
      const data = await listUsers({ page: query.page, pageSize: query.pageSize, keyword: query.keyword })
      return { list: data.list, total: data.total }
    }
    </script>

### 5.2 带左侧树（示例：按部门筛选职员）

    <template>
      <table-page
        :columns="columns"
        :fetch="fetchStaff"
        :search-fields="searchFields"
        :tree-data="deptTree"
        :tree-label-key="'name'"
        :tree-node-key="'id'"
        tree-title="部门"
        tree-query-field="deptId"
        @add="openDialog()"
        @tree-change="(node) => console.log('当前部门', node)"
      >
        <template #status="{ row }">
          <el-tag type="success" size="small">{{ row.status }}</el-tag>
        </template>
      </table-page>
    </template>

    <script setup lang="ts">
    import { TablePage } from '@/components/table-page'
    import type { SearchField, TableQuery } from '@/components/table-page'

    const searchFields: SearchField[] = [
      { field: 'name', label: '姓名', placeholder: '请输入姓名' },
      { field: 'status', label: '状态', type: 'select', options: [
        { label: '正常', value: 'normal' }, { label: '停用', value: 'disabled' },
      ] },
      { field: 'joinRange', label: '入职日期', type: 'daterange' },
    ]

    const columns = [
      { type: 'seq', title: '#', width: 60 },
      { field: 'name', title: '姓名', minWidth: 120 },
      { field: 'phone', title: '手机号', minWidth: 140 },
      { title: '状态', width: 90, slots: { default: 'status' } },
    ]

    async function fetchStaff(query: TableQuery) {
      // query 中自动包含 page/pageSize + 搜索字段 + 树选中值(deptId)
      const data = await api({ page: query.page, pageSize: query.pageSize, name: query.name, deptId: query.deptId })
      return { list: data.list, total: data.total }
    }
    </script>

## 6. 约定与注意事项

- 表格高度：外层容器需给高度(height:100% 或 flex:1)，height='auto' 才会正确撑满；
  固定高度可直接传 gridHeight=520 之类的数值。
- 分页参数：fetch 收到的 query.page 从 1 开始；搜索/重置/树切换会重置到第一页，刷新保持当前页。
- 分页条渲染依赖 vxe-pc-ui 的 VxeUI 插件注册（本项目 main.ts 已注册 app.use(VxeUI)），
  抽取组件到其他工程时需同步注册，否则分页条为空。
- 列插槽：columns 中 slots.default 的插槽名不要与 toolbar/tools 重名。
- 日期范围：daterange/datetimerange 的 v-model 为数组 [start,end]，提交前按后端约定自行转换。
- 树选中值：默认以 treeQueryField(默认 treeId) 合并进查询参数，点「全部」时该字段不传。
- 请求失败：fetch reject 时 vxe 会显示错误状态，错误信息由全局 axios 拦截器统一提示。
- 组件不内置新增/编辑弹窗：弹窗表单由业务页面自行实现(与现有用户/角色页一致)。

## 7. 扩展点

- 工具条：通过 #toolbar / #tools 插槽增加导出、批量操作等按钮
- 表格：columns 支持 vxe-grid 全部能力(排序 sortable、筛选 filters、行编辑 edit-config 等)
- 树：TablePageTree 可独立用于「左侧分类 + 右侧任意内容」的页面
- 后续可扩展：列设置(custom-config)、密度切换、导出按钮内置工具
