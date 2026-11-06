import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'

async function loadPureModule(relativePath) {
  const source = await readFile(new URL(relativePath, import.meta.url), 'utf8')
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2020 },
  })
  return import('data:text/javascript;base64,' + Buffer.from(outputText).toString('base64'))
}

const { businessTypeForField, schemaToPayload, parseFormSchema, renameSchemaField, moveSchemaField,
  orderedFields, fieldHasReferences, parseOptionValue } = await loadPureModule('../src/views/system/formgen/schema-payload.ts')
const { canApplyPreview, requestFingerprint } = await loadPureModule('../src/views/system/codegen/preview-state.ts')

test('JSON Schema types map to business types, including enum/date-time and explicit original metadata', () => {
  for (const [field, expected] of [
    [{ type: 'number' }, 'float'], [{ type: 'integer' }, 'int'], [{ type: 'boolean' }, 'bool'],
    [{ type: 'string', format: 'date-time' }, 'datetime'], [{ type: 'string', enum: ['A', 'B'] }, 'enum'],
    [{ type: 'string', 'x-ui': { control: 'textarea' } }, 'text'],
    [{ type: 'integer', 'x-fde': { type: 'relation' } }, 'relation'],
    [{ type: 'string', enum: ['A'], 'x-fde': { type: 'string' } }, 'string'],
  ]) assert.equal(businessTypeForField(field), expected)
})

test('save payload preserves complete schema and metadata without mutating the edited draft', () => {
  const schema = {
    objectKey: 'biz_ticket', name: '工单', version: 7, activeVersion: 3,
    'x-project-extension': { render: 'custom', nested: [1, { a: true }] },
    fields: {
      amount: { type: 'number', minimum: 0, maximum: 99, default: 2.5, multipleOf: 0.5,
        rules: { customRule: 'positive' }, 'x-extra': { retained: true },
        'x-fde': { type: 'float', searchable: true, inList: true, inForm: false, sort: 9 },
        'x-ui': { control: 'number', step: 0.5, vendorExtension: { a: 1 } } },
      status: { type: 'integer', enum: [1, 2], 'x-fde': { type: 'enum', sort: 2 },
        'x-ui': { options: [{ label: '新建', value: 1, color: 'green' }] } },
      owner: { type: 'integer', 'x-fde': { type: 'relation', sort: 5 } },
    },
    'x-relations': [{ field: 'owner', targetObject: 'sys_user', kind: 'many_to_one', custom: 'kept' }],
    'x-linkages': [{ when: { field: 'status', eq: 2 }, then: { field: 'amount', required: true, value: 10 }, custom: true }],
  }
  const before = JSON.stringify(schema)
  const payload = schemaToPayload(parseFormSchema(before), 'update', 7)
  assert.deepEqual(payload.schema, schema)
  assert.deepEqual(payload.relations, schema['x-relations'])
  assert.deepEqual(payload.linkages, schema['x-linkages'])
  assert.equal(payload.expectedVersion, 7)
  assert.deepEqual(payload.fields.map((field) => field.name), ['status', 'owner', 'amount'])
  const amount = payload.fields[2]
  assert.deepEqual([amount.searchable, amount.inList, amount.inForm, amount.sort], [true, true, false, 9])
  assert.deepEqual(amount.rules, { customRule: 'positive', minimum: 0, maximum: 99, default: 2.5, multipleOf: 0.5 })
  assert.deepEqual(payload.fields[0].rules.enum, [1, 2])
  payload.schema.fields.amount['x-extra'].retained = false
  assert.equal(JSON.stringify(schema), before)
})

test('unsupported field types and invalid JSON shape fail explicitly instead of saving a stale draft', () => {
  assert.throws(() => parseFormSchema('{broken'))
  assert.throws(() => parseFormSchema('{"objectKey":"x","name":"x","fields":{"a":null}}'))
  assert.throws(() => parseFormSchema('{"objectKey":"x","name":"x","fields":{},"x-linkages":{}}'), /x-linkages/)
  assert.throws(() => parseFormSchema('{"objectKey":"x","name":"x","fields":{},"x-relations":[null]}'), /x-relations/)
  assert.equal(parseFormSchema('{"objectKey":"x","name":"x","fields":{},"x-relations":null,"x-linkages":null}')['x-relations'], null)
  const schema = { objectKey: 'biz_x', name: 'x', fields: { nested: { type: 'object', properties: { a: { type: 'string' } } } } }
  const original = JSON.stringify(schema)
  assert.throws(() => schemaToPayload(schema, 'create'), /暂不支持/)
  assert.equal(JSON.stringify(schema), original)
})

test('field rename updates supported references and leaves custom configuration and original draft intact', () => {
  const schema = { objectKey: 'biz_x', name: 'x', fields: {
    state: { type: 'string', custom: { x: true } }, amount: { type: 'number' },
  }, 'x-linkages': [{ when: { field: 'state', eq: 'A', custom: 1 }, then: { field: 'amount', required: true } }],
  'x-relations': [{ field: 'state', targetObject: 'target', kind: 'many_to_one' }], custom: { untouched: true } }
  assert.equal(fieldHasReferences(schema, 'state'), true)
  const renamed = renameSchemaField(schema, 'state', 'status')
  assert.equal(renamed['x-linkages'][0].when.field, 'status')
  assert.equal(renamed['x-relations'][0].field, 'status')
  assert.deepEqual(renamed.fields.status.custom, { x: true })
  assert.deepEqual(renamed.custom, { untouched: true })
  assert.equal(schema['x-linkages'][0].when.field, 'state')
  assert.throws(() => renameSchemaField(schema, 'state', 'amount'), /已存在/)
  assert.throws(() => renameSchemaField(schema, 'state', 'State'), /小写/)
})

test('field reordering persists sort values despite JSON object key ordering', () => {
  const schema = { objectKey: 'biz_x', name: 'x', fields: {
    alpha: { type: 'string', 'x-fde': { sort: 1, searchable: true } },
    zeta: { type: 'string', 'x-fde': { sort: 0, custom: 'kept' } },
  } }
  assert.deepEqual(orderedFields(schema).map(([key]) => key), ['zeta', 'alpha'])
  const moved = moveSchemaField(schema, 'alpha', -1)
  assert.deepEqual(orderedFields(moved).map(([key]) => key), ['alpha', 'zeta'])
  assert.equal(moved.fields.alpha['x-fde'].searchable, true)
  assert.equal(moved.fields.zeta['x-fde'].custom, 'kept')
  assert.equal(moved.fields.alpha['x-fde'].sort, 0)
})

test('editing option labels/values preserves numeric and boolean enum values', () => {
  assert.equal(parseOptionValue('2', 1), 2)
  assert.equal(parseOptionValue('false', true), false)
  assert.equal(parseOptionValue('02', '01'), '02')
  assert.throws(() => parseOptionValue('abc', 1))
  assert.throws(() => parseOptionValue('', 1))
  assert.throws(() => parseOptionValue('no', false))
})

test('applying code requires a successful batch for exactly the current generation inputs', () => {
  const request = { tableName: 'biz_ticket', business: '工单', parentMenuId: 1,
    fields: [{ name: 'title', type: 'string', required: true }] }
  const fingerprint = requestFingerprint(request)
  const preview = { ok: true, batchId: 'batch-123', files: [] }
  assert.equal(canApplyPreview(preview, fingerprint, requestFingerprint(structuredClone(request))), true)
  for (const changed of [
    { ...request, tableName: 'biz_other' }, { ...request, parentMenuId: 2 },
    { ...request, fields: [{ ...request.fields[0], required: false }] },
  ]) assert.equal(canApplyPreview(preview, fingerprint, requestFingerprint(changed)), false)
  assert.equal(canApplyPreview(null, fingerprint, fingerprint), false)
  assert.equal(canApplyPreview({ ...preview, ok: false }, fingerprint, fingerprint), false)
  assert.equal(canApplyPreview({ ...preview, batchId: '' }, fingerprint, fingerprint), false)
  assert.equal(canApplyPreview({ ...preview, diff: { conflicts: ['modified file'] } }, fingerprint, fingerprint), false)
})
