import { describe, expect, it } from 'vitest'
import { canonicalPath, mappingRecordFromRows, mappingRowsFromRecord } from './pathMappings'

describe('path mapping helpers', () => {
  it('normalizes optional v1 path prefixes', () => {
    expect(canonicalPath('/v1/responses/compact/')).toBe('responses/compact')
  })

  it('builds normalized records and omits blank rows', () => {
    expect(mappingRecordFromRows([
      { id: 1, source: '/v1/responses/compact/', target: '/api/paas/v4/chat/completions/' },
      { id: 2, source: '', target: '' },
    ])).toEqual({ 'responses/compact': 'api/paas/v4/chat/completions' })
  })

  it('converts records into editable rows', () => {
    expect(mappingRowsFromRecord({ models: 'api/paas/v4/models' }, () => 7)[0]).toEqual({
      id: 7,
      source: 'models',
      target: 'api/paas/v4/models',
    })
  })
})
