import type { FormSchema, LinkageRule } from './types'

export interface LinkageState {
  visible: Record<string, boolean>
  required: Record<string, boolean>
}

// 检测 x-linkages 的 when.field -> then.field 有向图中的环。
// 返回所有位于环上的字段；这些字段参与的联动规则会被忽略。
export function detectLinkageCycles(linkages: LinkageRule[]): Set<string> {
  const graph = new Map<string, string[]>()
  for (const rule of linkages) {
    const from = rule.when?.field
    const to = rule.then?.field
    if (!from || !to) continue
    const next = graph.get(from)
    if (next) {
      next.push(to)
    } else {
      graph.set(from, [to])
    }
  }

  const cyclic = new Set<string>()
  const color = new Map<string, 0 | 1 | 2>()
  const stack: string[] = []

  const visit = (node: string): void => {
    color.set(node, 1)
    stack.push(node)
    for (const next of graph.get(node) ?? []) {
      const state = color.get(next) ?? 0
      if (state === 0) {
        visit(next)
      } else if (state === 1) {
        const start = stack.indexOf(next)
        if (start >= 0) {
          for (let i = start; i < stack.length; i++) {
            cyclic.add(stack[i])
          }
        }
      }
    }
    stack.pop()
    color.set(node, 2)
  }

  for (const node of graph.keys()) {
    if ((color.get(node) ?? 0) === 0) {
      visit(node)
    }
  }
  return cyclic
}

function matchesEq(value: unknown, eq: unknown): boolean {
  if (value === eq) return true
  if (value === null || value === undefined || eq === null || eq === undefined) return false
  return String(value) === String(eq)
}

function resetState(schema: FormSchema, state: LinkageState): void {
  for (const name of Object.keys(state.visible)) {
    state.visible[name] = true
  }
  for (const [name, field] of Object.entries(schema.fields ?? {})) {
    state.visible[name] = true
    state.required[name] = field.required === true
  }
}

// 重置为基础状态后，按数组顺序执行匹配 when 的 then。
// 位于环上的字段相关规则被忽略，保证不会形成循环更新或崩溃。
export function applyLinkages(schema: FormSchema, model: Record<string, any>, state: LinkageState): void {
  resetState(schema, state)
  const cyclic = detectLinkageCycles(schema['x-linkages'] ?? [])
  for (const rule of schema['x-linkages'] ?? []) {
    const whenField = rule.when?.field
    const thenField = rule.then?.field
    if (!whenField || !thenField) continue
    if (cyclic.has(whenField) || cyclic.has(thenField)) continue
    if (!matchesEq(model[whenField], rule.when?.eq)) continue
    if (rule.then.visible !== undefined) {
      state.visible[thenField] = rule.then.visible
    }
    if (rule.then.required !== undefined) {
      state.required[thenField] = rule.then.required
    }
  }
}

export function buildLinkageState(schema: FormSchema, model: Record<string, any>): LinkageState {
  const state: LinkageState = { visible: {}, required: {} }
  for (const name of Object.keys(schema.fields ?? {})) {
    state.visible[name] = true
    state.required[name] = schema.fields[name]?.required === true
  }
  applyLinkages(schema, model, state)
  return state
}
